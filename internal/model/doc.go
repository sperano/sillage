// Package model defines sillage's normalized, source-independent records:
// events (what happened, when, by which actor and executor, with the
// evidence behind it), runs (one Kelos Task attempt, identified by cluster,
// namespace and Task UID), entities and the links between them (tasks and
// pull requests), and coverage (which time ranges each source is
// known to have been read for, so that a gap stays visibly unknown).
//
// BuildStory assembles the records about one Vikunja task into a Story:
// validated, in UTC, in a deterministic order. The fabricated stories under
// testdata/fixtures/stories are its golden tests and the shared contract
// between adapters and the web UI.
//
// Every source adapter produces these types and every consumer (store, API,
// web UI) reads them; nothing here knows how a source is queried.
package model
