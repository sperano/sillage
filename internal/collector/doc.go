// Package collector assembles one story from the live sources: the
// Vikunja task or wiki page it is rooted in, the Kelos runs that belong to
// it, the pull requests of its branch and the wiki pages its comments
// link, with the correlations that join them and one coverage record per
// source instance.
//
// It joins records only on evidence, and says which: an identifier both
// records carry (a Task label, a pod name, a branch) or a heuristic (a
// Task name convention, an agent's account acting inside a run's
// interval). When the evidence fits several runs, each keeps its own
// correlation, flagged ambiguous; nothing is merged. A source that cannot
// be read contributes no record and a coverage without a read, so a story
// never looks complete when it is not.
//
// It reads once and keeps nothing: storing, polling and webhooks come
// later.
package collector
