// Package model defines sillage's normalized, source-independent records:
// events (what happened, when, by which actor and executor, with the
// evidence behind it), runs (one Kelos Task attempt, identified by cluster,
// namespace and Task UID), entities and the links between them (tasks, wiki
// pages, pull requests), and coverage (which time ranges each source is
// known to have been read for, so that a gap stays visibly unknown).
//
// Every source adapter produces these types and every consumer (store, API,
// web UI) reads them; nothing here knows how a source is queried.
package model
