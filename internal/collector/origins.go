package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
)

// OriginMemory keeps what started each Kelos Task (its kelos.Origin, as
// JSON) past the Task's TTL: a TaskRecord copies the Task's labels but not
// its annotations. The store implements it.
type OriginMemory interface {
	// KeepOrigins stores each run's origin unless it holds one already:
	// the first read that saw the Task wins.
	KeepOrigins(ctx context.Context, origins map[model.RunKey][]byte) error
	// RecallOrigins returns the origins kept of runs, leaving out those
	// it has none of.
	RecallOrigins(ctx context.Context, runs []model.RunKey) (map[model.RunKey][]byte, error)
}

// Option configures a Collector.
type Option func(*Collector)

// WithOriginMemory makes every Kelos read keep the Origin of each Task it
// lists in m, and recall from m the Origin of each run it reads from a
// TaskRecord alone, so that the run is still joined by its annotations
// once its Task is gone. Without it, or with a nil m, such a run is joined
// by its labels, branch and name only.
//
// Every read keeps every listed Task's Origin again: m keeps the first,
// so a repeat writes nothing, and an Origin m pruned while its Task was
// still listed is kept again.
func WithOriginMemory(m OriginMemory) Option {
	return func(c *Collector) { c.origins = m }
}

// remember keeps the origins of the Tasks snap lists in m, and returns
// snap with the origins m kept of the runs it reads from a TaskRecord
// alone. With no m, it returns snap.
func remember(ctx context.Context, m OriginMemory, cfg kelos.Config, snap kelos.Snapshot) (kelos.Snapshot, error) {
	if m == nil {
		return snap, nil
	}
	if err := keepOrigins(ctx, m, kelos.Origins(cfg, snap)); err != nil {
		return kelos.Snapshot{}, fmt.Errorf("keeping the Tasks' origins: %w", err)
	}
	gone := kelos.RecordOnlyRuns(cfg, snap)
	if len(gone) == 0 {
		return snap, nil
	}
	kept, err := m.RecallOrigins(ctx, gone)
	if err != nil {
		return kelos.Snapshot{}, fmt.Errorf("recalling the origins of deleted Tasks: %w", err)
	}
	snap.Remembered = make(map[types.UID]kelos.Origin, len(kept))
	for k, body := range kept {
		var o kelos.Origin
		if err := json.Unmarshal(body, &o); err != nil {
			return kelos.Snapshot{}, fmt.Errorf("the origin kept of run %s: %w", k, err)
		}
		snap.Remembered[types.UID(k.UID)] = o
	}
	return snap, nil
}

// keepOrigins stores origins in m, as JSON.
func keepOrigins(ctx context.Context, m OriginMemory, origins map[model.RunKey]kelos.Origin) error {
	if len(origins) == 0 {
		return nil
	}
	bodies := make(map[model.RunKey][]byte, len(origins))
	for k, o := range origins {
		body, err := json.Marshal(o)
		if err != nil {
			return err
		}
		bodies[k] = body
	}
	return m.KeepOrigins(ctx, bodies)
}

// originOf returns the Origin of run r, nil when it has none.
func (a *assembly) originOf(r model.Run) *kelos.Origin {
	if a.kelos == nil {
		return nil
	}
	return a.kelos.placements[r.Key].Origin
}

// annotatedLinks returns the correlations Normalize made of r's Origin
// with records of kind.
func (a *assembly) annotatedLinks(r model.Run, kind model.RefKind) []model.Correlation {
	var out []model.Correlation
	for _, c := range a.kelos.linksFrom[r.Key.Ref()] {
		if c.To.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// requestLinks joins each run of the story to the comment or review that
// asked for it, as its Origin names it: an identifier. A request the story
// does not hold is left out with the other outside records.
func (a *assembly) requestLinks() {
	for _, r := range a.runs {
		o := a.originOf(r)
		if o == nil {
			continue
		}
		if e, names, ok := requestEvent(*o); ok {
			a.links.identifier(r.Key.Ref(), e.Ref(), o.Evidence(a.cfg.Kelos, names...))
		}
	}
}

// requestEvent returns the event an Origin names as the request that
// started its Task, and the annotations that name it.
func requestEvent(o kelos.Origin) (model.Event, []string, bool) {
	host := strings.ToLower(o.Value(kelos.AnnotationSourceInstance))
	if host == "" {
		return model.Event{}, nil, false
	}
	switch o.Value(kelos.AnnotationSource) {
	case kelos.OriginVikunja:
		return commentRequest(o, model.SourceVikunja, host)
	case kelos.OriginGitHub:
		return githubRequest(o, host)
	}
	return model.Event{}, nil, false
}

// commentRequest returns the Vikunja comment an Origin names.
func commentRequest(o kelos.Origin, source model.Source, host string) (model.Event, []string, bool) {
	id, ok := o.ID(kelos.AnnotationComment)
	names := []string{kelos.AnnotationSource, kelos.AnnotationSourceInstance, kelos.AnnotationComment}
	return model.Event{Source: source, Instance: host, ID: commentObject(id)}, names, ok
}

// githubRequest returns the comment or review a GitHub event's Origin
// names, by its event annotation.
func githubRequest(o kelos.Origin, host string) (model.Event, []string, bool) {
	names := []string{kelos.AnnotationSource, kelos.AnnotationSourceInstance, kelos.AnnotationEvent}
	comment, hasComment := o.ID(kelos.AnnotationComment)
	e := model.Event{Source: model.SourceGitHub, Instance: host}
	switch o.Value(kelos.AnnotationEvent) {
	case kelos.EventIssueComment:
		e.ID = github.CommentEventID(comment)
		return e, append(names, kelos.AnnotationComment), hasComment
	case kelos.EventReviewComment:
		e.ID = github.ReviewCommentEventID(comment)
		return e, append(names, kelos.AnnotationComment), hasComment
	case kelos.EventReview:
		review, ok := o.ID(kelos.AnnotationReview)
		e.ID = github.ReviewEventID(review)
		return e, append(names, kelos.AnnotationReview), ok
	}
	return model.Event{}, nil, false
}

// originRepository returns the repository r's Origin names, if any. It is
// read whether or not a workspace maps to it: the spawners that create the
// Tasks name it, and the GitHub token's read scopes bound what any name
// can reach.
func (a *assembly) originRepository(r model.Run) (github.Repository, bool) {
	o := a.originOf(r)
	if o == nil || o.Value(kelos.AnnotationRepository) == "" {
		return github.Repository{}, false
	}
	repo, err := github.ParseRepository(o.Value(kelos.AnnotationRepository))
	return repo, err == nil
}
