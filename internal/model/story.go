package model

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Records are the normalized records a story is assembled from, in any
// order: what source adapters produce and the store returns.
type Records struct {
	Events       []Event       `json:"events"`
	Runs         []Run         `json:"runs"`
	PullRequests []PullRequest `json:"pullRequests"`
	Correlations []Correlation `json:"correlations"`
	Coverage     []Coverage    `json:"coverage"`
	// Markers are the phase markers joined to the runs, which BuildStory
	// pairs into the runs' phases. A story holds the phases, not the
	// markers.
	Markers []Marker `json:"markers,omitempty"`
}

// Story is everything known about one root, validated, with every time in
// UTC and every list in a deterministic order, whatever the order of the
// records it was built from.
type Story struct {
	Root Root `json:"root"`
	Records
}

// BuildStory validates the records and returns them as root's story. It
// rejects two records with the same identity rather than choosing one:
// deduplicating source deliveries is the collector's job, and two runs
// with one key would mean a retry was merged. Each run's phases are those
// its markers establish, whatever phases recs held, and the Kelos coverage
// of their cluster notes that markers are best effort. It does not modify
// recs.
func BuildStory(root Root, recs Records) (Story, error) {
	if err := root.Validate(); err != nil {
		return Story{}, err
	}
	recs = recs.clone()
	if err := recs.validate(); err != nil {
		return Story{}, err
	}
	recs.normalize()
	recs.sort()
	recs.addPhases()
	recs.notePhases()
	recs.Markers = nil
	return Story{Root: root, Records: recs}, nil
}

func (r Records) validate() error {
	return errors.Join(
		validateAll(r.Events, Event.Validate, Event.Key),
		validateAll(r.Runs, Run.Validate, func(run Run) string { return run.Key.String() }),
		validateRunPods(r.Runs),
		validateAll(r.PullRequests, PullRequest.Validate, func(p PullRequest) string { return p.Key.String() }),
		validateAll(r.Correlations, Correlation.Validate, Correlation.Pair),
		validateAll(r.Coverage, Coverage.Validate, Coverage.Key),
		validateMarkers(r.Markers, r.Runs),
	)
}

// validateAll validates each record and rejects a key seen twice.
func validateAll[T any, K comparable](records []T, validate func(T) error, key func(T) K) error {
	var errs []error
	seen := make(map[K]bool, len(records))
	for _, rec := range records {
		if err := validate(rec); err != nil {
			errs = append(errs, err)
			continue
		}
		k := key(rec)
		if seen[k] {
			errs = append(errs, fmt.Errorf("%v: duplicate record", k))
		}
		seen[k] = true
	}
	return errors.Join(errs...)
}

// validateRunPods rejects a run keyed by pod when a run keyed by UID names
// the same pod: they are one attempt, which the collector must re-key by UID
// rather than keep twice.
func validateRunPods(runs []Run) error {
	byUID := make(map[RunKey]bool)
	for _, r := range runs {
		if r.Key.UID != "" && r.Pod != "" {
			byUID[RunKey{Cluster: r.Key.Cluster, Namespace: r.Key.Namespace, Pod: r.Pod}] = true
		}
	}
	var errs []error
	for _, r := range runs {
		if r.Key.Pod != "" && byUID[r.Key] {
			errs = append(errs, fmt.Errorf("run %s: a run keyed by UID names the same pod", r.Key))
		}
	}
	return errors.Join(errs...)
}

// clone copies every slice and map a story holds, so that neither
// normalize and sort nor a caller changing the story can modify the input.
// A nil list becomes empty, so that a story's lists marshal as [], never
// null.
func (r Records) clone() Records {
	out := Records{
		Events:       cloneList(r.Events),
		Runs:         cloneList(r.Runs),
		PullRequests: cloneList(r.PullRequests),
		Correlations: cloneList(r.Correlations),
		Coverage:     cloneList(r.Coverage),
		Markers:      slices.Clone(r.Markers),
	}
	for i := range out.Events {
		out.Events[i].Details = maps.Clone(out.Events[i].Details)
	}
	for i := range out.Runs {
		out.Runs[i].Evidence = cloneList(out.Runs[i].Evidence)
		out.Runs[i].Usage = out.Runs[i].Usage.Clone()
		if c := out.Runs[i].Created; c != nil {
			created := *c
			out.Runs[i].Created = &created
		}
	}
	for i := range out.PullRequests {
		out.PullRequests[i].Checks = cloneList(out.PullRequests[i].Checks)
	}
	for i := range out.Correlations {
		out.Correlations[i].Evidence = cloneList(out.Correlations[i].Evidence)
	}
	for i := range out.Coverage {
		out.Coverage[i].Gaps = slices.Clone(out.Coverage[i].Gaps)
		out.Coverage[i].Notes = slices.Clone(out.Coverage[i].Notes)
	}
	return out
}

// cloneList copies s; a nil s becomes an empty slice.
func cloneList[S ~[]E, E any](s S) S {
	if s == nil {
		return S{}
	}
	return slices.Clone(s)
}

func (r *Records) normalize() {
	for i := range r.Events {
		r.Events[i].normalize()
	}
	for i := range r.Runs {
		r.Runs[i].normalize()
	}
	for i := range r.PullRequests {
		r.PullRequests[i].normalize()
	}
	for i := range r.Correlations {
		sortEvidence(r.Correlations[i].Evidence)
	}
	for i := range r.Coverage {
		r.Coverage[i].normalize()
	}
}
