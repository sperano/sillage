package kelos

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
)

// The names, after the configured domain, of the correlation annotations
// the spawners put on Tasks (the Hollingsworth chart's
// templates/_correlation.tpl): what started each Task. The chart leaves a
// name that does not apply to an event off. A TaskRecord does not copy
// annotations, so they are only read from a Task, or from an Origin its
// reader kept.
const (
	// AnnotationSource names what sent the event: one of the Origin*
	// values.
	AnnotationSource = "source"
	// AnnotationSourceInstance is that source's host.
	AnnotationSourceInstance = "source-instance"
	// AnnotationRepository is the workspace's repository, owner/name.
	AnnotationRepository = "repository"
	// AnnotationPR is a pull request number.
	AnnotationPR = "pr"
	// AnnotationEvent is the GitHub event (one of the Event* values).
	AnnotationEvent = "event"
	// AnnotationComment is the id of the comment that asked for the Task,
	// on the source's instance.
	AnnotationComment = "comment"
	// AnnotationReview is a GitHub review id.
	AnnotationReview = "review"
	// AnnotationBranch is the head branch, on the Tasks with no
	// spec.branch (merge-close, pr-open).
	AnnotationBranch = "branch"
	// AnnotationWikiPage, AnnotationWikiPath and AnnotationWikiLocale are
	// a Wiki.js page's id, path and locale.
	AnnotationWikiPage   = "wiki-page"
	AnnotationWikiPath   = "wiki-path"
	AnnotationWikiLocale = "wiki-locale"
	// AnnotationDelivery is the id of the delivery an internal dispatcher
	// (wiki poller, review poller, conflict reconciler) made.
	AnnotationDelivery = "delivery"
)

// correlationAnnotations are the annotations an Origin keeps. Any other
// annotation is dropped: kubectl's last-applied-configuration, for one,
// holds the prompt.
var correlationAnnotations = []string{
	AnnotationSource, AnnotationSourceInstance, AnnotationRepository, AnnotationPR, AnnotationEvent,
	AnnotationComment, AnnotationReview, AnnotationBranch, AnnotationWikiPage, AnnotationWikiPath,
	AnnotationWikiLocale, AnnotationDelivery,
}

// The values of the source annotation.
const (
	OriginVikunja  = "vikunja"
	OriginGitHub   = "github"
	OriginWiki     = "wiki"
	OriginSchedule = "schedule"
)

// The values of the event annotation that name a comment or a review.
const (
	EventIssueComment  = "issue_comment"
	EventReview        = "pull_request_review"
	EventReviewComment = "pull_request_review_comment"
)

// Origin is what started a Task, as its correlation annotations say. A
// reader that needs it after the Task's TTL keeps it (as JSON) when it
// first reads the Task, and gives it back in Snapshot.Remembered.
type Origin struct {
	// Task locates the Task the annotations were read from.
	Task string `json:"task"`
	// Annotations holds the correlation annotations by name after the
	// domain (AnnotationSource, ...), only those set.
	Annotations map[string]string `json:"annotations"`
	// SeenAt is when they were first read: the read's ObservedAt.
	SeenAt time.Time `json:"seenAt"`
	// remembered is set on an Origin from Snapshot.Remembered.
	remembered bool
}

// Value returns the annotation of name, empty when it is not set.
func (o Origin) Value(name string) string { return o.Annotations[name] }

// ID returns the positive id the annotation of name holds, and whether it
// holds one.
func (o Origin) ID(name string) (int64, bool) {
	id, err := strconv.ParseInt(o.Annotations[name], 10, 64)
	return id, err == nil && id > 0
}

// Note says which annotations, in the order given, an evidence entry rests on,
// and, for an Origin a reader kept, that it was read from the Task before
// the Task was deleted.
func (o Origin) Note(cfg Config, names ...string) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, cfg.label(name)+"="+o.Annotations[name])
	}
	note := "annotation " + strings.Join(parts, ", ")
	if o.remembered {
		note += "; read from the Task at " + o.SeenAt.UTC().Format(time.RFC3339) +
			" and kept by sillage: the Task is gone, and its TaskRecord does not copy annotations"
	}
	return note
}

// Evidence is an evidence entry of the Kelos source resting on the
// annotations of names.
func (o Origin) Evidence(cfg Config, names ...string) model.Evidence {
	return model.Evidence{Source: model.SourceKelos, Instance: cfg.Cluster, Ref: o.Task, Note: o.Note(cfg, names...)}
}

// originOf returns the Origin a Task's annotations give, read at at, and
// whether it has any correlation annotation.
func (c Config) originOf(t *Task, at time.Time) (Origin, bool) {
	values := c.correlationAnnotations(t.Annotations)
	if len(values) == 0 {
		return Origin{}, false
	}
	return Origin{Task: c.taskRef(t.Name), Annotations: values, SeenAt: at}, true
}

// correlationAnnotations returns the correlation annotations among
// annotations, by name after the domain. Empty values are left out, and so
// are values holding a control character: the spawners set ids and names
// only, and a NUL would make every write of the Origin fail (PostgreSQL's
// jsonb refuses it).
func (c Config) correlationAnnotations(annotations map[string]string) map[string]string {
	out := make(map[string]string)
	for _, name := range correlationAnnotations {
		v := strings.TrimSpace(annotations[c.label(name)])
		if v != "" && !strings.ContainsFunc(v, unicode.IsControl) {
			out[name] = v
		}
	}
	return out
}

// keptAnnotations returns the correlation annotations among annotations,
// under their full keys, or nil when there are none.
func (c Config) keptAnnotations(annotations map[string]string) map[string]string {
	values := c.correlationAnnotations(annotations)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for name, v := range values {
		out[c.label(name)] = v
	}
	return out
}

// Origins returns the Origin of each Task of s that carries a correlation
// annotation, by run key: what a reader keeps for when the Task is gone.
func Origins(cfg Config, s Snapshot) map[model.RunKey]Origin {
	out := make(map[model.RunKey]Origin)
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.UID == "" {
			continue
		}
		if o, ok := cfg.originOf(t, s.ObservedAt); ok {
			out[cfg.runKey(t.UID)] = o
		}
	}
	return out
}

// RecordOnlyRuns returns the key of each run of s read from its
// TaskRecord alone, in key order: those whose Origin a reader may have
// kept.
func RecordOnlyRuns(cfg Config, s Snapshot) []model.RunKey {
	listed := make(map[types.UID]bool, len(s.Tasks))
	for _, t := range s.Tasks {
		listed[t.UID] = true
	}
	seen := make(map[model.RunKey]bool)
	for _, r := range s.Records {
		if uid := r.Spec.TaskRef.UID; uid != "" && !listed[uid] {
			seen[cfg.runKey(uid)] = true
		}
	}
	return slices.SortedFunc(maps.Keys(seen), func(a, b model.RunKey) int { return strings.Compare(a.UID, b.UID) })
}

// origin returns the attempt's Origin: its Task's annotations, else the
// Origin a reader kept of it.
func (c Config) origin(a *attempt) (Origin, bool) {
	if a.task != nil {
		return c.originOf(a.task, time.Time{})
	}
	if a.remembered == nil || len(a.remembered.Annotations) == 0 {
		return Origin{}, false
	}
	o := *a.remembered
	o.remembered = true
	return o, true
}

func (c Config) runKey(uid types.UID) model.RunKey {
	return model.RunKey{Cluster: c.Cluster, Namespace: c.Namespace, UID: string(uid)}
}
