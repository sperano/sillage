// Package collector assembles one story from the live sources: the
// Vikunja task it is rooted in, the Kelos runs that belong to it and the
// pull requests of its branch, with the correlations that join them and one
// coverage record per source instance.
//
// It joins records only on evidence, and says which: an identifier both
// records carry (a Task label, a pod name, a branch) or a heuristic (a
// Task name convention, an agent's account acting inside a run's
// interval). When the evidence fits several runs, each keeps its own
// correlation, flagged ambiguous; nothing is merged. A source that cannot
// be read contributes no record and a coverage without a read, so a story
// never looks complete when it is not.
//
// It reads once and keeps nothing. internal/reconcile reads it again and
// again into the store, and passes back what the store kept: the runs
// whose Tasks Kelos has since deleted, and since when Kelos was read.
package collector
