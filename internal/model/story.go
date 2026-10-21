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
// with one key would mean a retry was merged. It does not modify recs.
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
	return Story{Root: root, Records: recs}, nil
}

func (r Records) validate() error {
	return errors.Join(
		validateAll(r.Events, Event.Validate, Event.Key),
		validateAll(r.Runs, Run.Validate, func(run Run) string { return run.Key.String() }),
		validateAll(r.PullRequests, PullRequest.Validate, func(p PullRequest) string { return p.Key.String() }),
		validateAll(r.Correlations, Correlation.Validate, Correlation.String),
		validateAll(r.Coverage, Coverage.Validate, Coverage.Key),
	)
}

// validateAll validates each record and rejects a key seen twice.
func validateAll[T any](records []T, validate func(T) error, key func(T) string) error {
	var errs []error
	seen := make(map[string]bool, len(records))
	for _, rec := range records {
		if err := validate(rec); err != nil {
			errs = append(errs, err)
			continue
		}
		k := key(rec)
		if seen[k] {
			errs = append(errs, fmt.Errorf("%s: duplicate record", k))
		}
		seen[k] = true
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
	}
	for i := range out.Events {
		out.Events[i].Details = maps.Clone(out.Events[i].Details)
	}
	for i := range out.Runs {
		out.Runs[i].Evidence = cloneList(out.Runs[i].Evidence)
	}
	for i := range out.PullRequests {
		out.PullRequests[i].Checks = cloneList(out.PullRequests[i].Checks)
	}
	for i := range out.Correlations {
		out.Correlations[i].Evidence = cloneList(out.Correlations[i].Evidence)
	}
	for i := range out.Coverage {
		out.Coverage[i].Gaps = slices.Clone(out.Coverage[i].Gaps)
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
