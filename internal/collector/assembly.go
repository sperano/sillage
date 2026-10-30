package collector

import (
	"maps"
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
	refusals []Refusal
	// prLinks are the task's comments' links to pull requests; searches
	// counts the repositories its branch was looked for in.
	prLinks  []prLink
	searches int
	// kelos is the namespace's runs; nil until read, and when the read
	// failed.
	kelos *kelosView
	// announcedOnly holds when each run known only from its announcement
	// was announced.
	announcedOnly map[model.RunKey]time.Time
	// joined are the announcements announcedRuns has joined, and claimed
	// the runs of a Task gone that one of them was joined to by its time.
	joined  map[model.Ref]bool
	claimed map[model.RunKey]bool
	// kept is what a store already holds of the story, and keptOnly its
	// runs the Kelos read does not list.
	kept     Kept
	keptOnly []model.Run
}

// sourceState is what one source instance's reads covered.
type sourceState struct {
	cov    model.Coverage
	failed bool
}

func newAssembly(c *Collector, root model.Root) *assembly {
	return &assembly{
		c:       c,
		cfg:     c.cfg,
		root:    root,
		rootRef: root.Ref(),
		events:  make(map[string]model.Event),
		runs:    make(map[model.RunKey]model.Run),
		prs:     make(map[model.PRKey]model.PullRequest),
		links:   newLinks(),
		sources: make(map[model.Source]*sourceState),
		// announcedOnly is filled as announcements are read.
		announcedOnly: make(map[model.RunKey]time.Time),
		joined:        make(map[model.Ref]bool),
		claimed:       make(map[model.RunKey]bool),
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
func (a *assembly) instance(s model.Source) string { return a.cfg.Instance(s) }

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
// outside the story (another task a candidate run belongs to), and kept
// markers of runs outside it, are left out. It does not change the
// assembly.
func (a *assembly) build() (model.Story, error) {
	recs := model.Records{
		Events:       slices.Collect(maps.Values(a.events)),
		Runs:         slices.Collect(maps.Values(a.runs)),
		PullRequests: slices.Collect(maps.Values(a.prs)),
	}
	for s, st := range a.sources {
		cov := st.cov
		if s == model.SourceKelos {
			cov.Gaps = append(slices.Clone(cov.Gaps), a.kelosGaps(cov, recs)...)
		}
		recs.Coverage = append(recs.Coverage, cov)
	}
	recs.Correlations = a.links.within(recs.Refs(a.root))
	recs.Markers = a.keptMarkers(recs.Runs)
	return model.BuildStory(a.root, recs)
}
