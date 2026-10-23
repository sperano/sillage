package collector

import (
	"slices"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// assembly is one story being assembled: the records read so far, the
// correlations between them, and what each source's reads covered.
type assembly struct {
	c        *Collector
	cfg      Config
	root     model.Root
	rootRef  model.Ref
	events   map[string]model.Event
	runs     map[model.RunKey]model.Run
	prs      map[model.PRKey]model.PullRequest
	links    links
	sources  map[model.Source]*sourceState
	failures []Failure
	// kelos is the namespace's runs; nil until read, and when the read
	// failed.
	kelos *kelosView
	// announcedOnly holds when each run known only from its announcement
	// was announced.
	announcedOnly map[model.RunKey]time.Time
}

// sourceState is what one source instance's reads covered.
type sourceState struct {
	cov    model.Coverage
	failed bool
}

func newAssembly(c *Collector, root model.Root) *assembly {
	ref := model.Ref{}
	if root.Task != nil {
		ref = root.Task.Ref()
	} else {
		ref = root.Page.Ref()
	}
	return &assembly{
		c:       c,
		cfg:     c.cfg,
		root:    root,
		rootRef: ref,
		events:  make(map[string]model.Event),
		runs:    make(map[model.RunKey]model.Run),
		prs:     make(map[model.PRKey]model.PullRequest),
		links:   newLinks(),
		sources: make(map[model.Source]*sourceState),

		announcedOnly: make(map[model.RunKey]time.Time),
	}
}

// source returns the state of a source the story covers, adding it.
func (a *assembly) source(s model.Source) *sourceState {
	st, ok := a.sources[s]
	if !ok {
		st = &sourceState{cov: model.Coverage{Source: s, Instance: a.instance(s)}}
		a.sources[s] = st
	}
	return st
}

// instance names the configured instance of a source.
func (a *assembly) instance(s model.Source) string {
	switch s {
	case model.SourceVikunja:
		return a.cfg.VikunjaInstance
	case model.SourceKelos:
		return a.cfg.Kelos.Cluster
	case model.SourceGitHub:
		return a.cfg.GitHubInstance
	}
	return a.cfg.WikiInstance
}

// read records a successful read of s that started at at. With several
// reads, the earliest start is how far all are known to reach.
func (a *assembly) read(s model.Source, at time.Time) {
	st := a.source(s)
	if st.failed {
		return
	}
	if r := st.cov.LastSuccessfulRead; r == nil || at.Before(*r) {
		st.cov.LastSuccessfulRead = &at
	}
}

// fail records a failed read of s: its coverage loses any read, and the
// caller adds none of its records.
func (a *assembly) fail(s model.Source, err error) {
	st := a.source(s)
	st.failed = true
	st.cov.LastSuccessfulRead = nil
	a.failures = append(a.failures, Failure{Source: s, Instance: st.cov.Instance, Err: err})
}

// note adds a note to s's coverage.
func (a *assembly) note(s model.Source, note string) {
	st := a.source(s)
	st.cov.Notes = append(st.cov.Notes, note)
}

func (a *assembly) addEvents(events []model.Event) {
	for _, e := range events {
		a.events[e.Key()] = e
	}
}

func (a *assembly) addRun(r model.Run) { a.runs[r.Key] = r }

// eventsOf returns the story's events of kind, in key order.
func (a *assembly) eventsOf(kind model.EventKind) []model.Event {
	var out []model.Event
	for _, e := range a.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(x, y model.Event) int { return strings.Compare(x.Key(), y.Key()) })
	return out
}

// build validates the records into the story. Correlations that point
// outside the story (another task a candidate run belongs to) are left
// out.
func (a *assembly) build() (model.Story, error) {
	recs := model.Records{
		Events:       mapValues(a.events),
		Runs:         mapValues(a.runs),
		PullRequests: mapValues(a.prs),
	}
	a.kelosGaps(recs)
	for _, st := range a.sources {
		recs.Coverage = append(recs.Coverage, st.cov)
	}
	recs.Correlations = a.links.within(knownRefs(a.root, recs))
	return model.BuildStory(a.root, recs)
}

// knownRefs returns every record a correlation may point to: the root,
// the events and the tasks and pages they name, the runs and the pull
// requests.
func knownRefs(root model.Root, recs model.Records) map[model.Ref]bool {
	known := make(map[model.Ref]bool)
	addRelated := func(r model.Related) {
		if r.Task != nil {
			known[r.Task.Ref()] = true
		}
		if r.Page != nil {
			known[r.Page.Ref()] = true
		}
	}
	addRelated(model.Related{Task: root.Task, Page: root.Page})
	for _, e := range recs.Events {
		known[e.Ref()] = true
		addRelated(e.Related)
	}
	for _, r := range recs.Runs {
		known[r.Key.Ref()] = true
	}
	for _, p := range recs.PullRequests {
		known[p.Key.Ref()] = true
	}
	return known
}

func mapValues[K comparable, V any](m map[K]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
