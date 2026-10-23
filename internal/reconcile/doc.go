// Package reconcile keeps the store current without webhooks: a loop
// that reads the sources periodically, assembles the story of every root
// they name through the collector, and commits what changed, with how
// fresh each story's sources are and where their records are missing.
//
// Each cycle reads the Kelos namespace once and finds the roots its runs
// name, lists the configured Vikunja projects' tasks changed since the
// last listing, and collects every root that is new, whose runs changed,
// whose task changed, or that was not read for a refresh interval. Every
// successful read of a root's source advances that source's
// LastSuccessfulRead; a failed one opens a gap, from the last success to
// the next, with the reason; a restart opens one for every source of
// every root. A read the shutdown interrupts commits nothing.
//
// One process collects at a time: the loop runs under a lock, and a
// process that cannot take it reads nothing and says so.
package reconcile
