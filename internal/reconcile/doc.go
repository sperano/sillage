// Package reconcile keeps the store current: a loop that reads the
// sources periodically, assembles the story of every root they name
// through the collector, and commits what changed, with how fresh each
// story's sources are and where their records are missing.
//
// Each cycle reads the Kelos namespace once and finds the roots its runs
// name, lists the configured Vikunja projects' tasks changed since the
// last listing, and collects every root that is new, whose runs changed,
// whose task changed, whose Kelos gap is open, or that was not read for a
// refresh interval.
//
// A root's GitHub side has a schedule of its own, since its reads cost
// the token's rate limit: it is read when a delivery names the root, a
// run is new or changed, or the task changed. A refresh, or a Kelos gap,
// keeps the stored records of its last read instead. Past a
// BackstopInterval with none of those, the backstop asks GitHub, with
// the ETags of the last read (ConditionalGitHub), whether its lists
// changed: 304 Not Modified, which the rate limit does not count, closes
// the coverage there; a change reads it whole. A settled root (task done,
// pull requests closed or merged, runs finished) gets no backstop unless
// a gap is open on its GitHub coverage, and no backstop read is made
// while the rate limit's window has less than githubReserve left. A
// restart spreads the backstop reads it finds overdue over the interval.
//
// The roots webhook deliveries name (Deliveries) are
// read too, first, at most DeliveryRootsPerCycle a cycle, the others left
// queued for the next, and each once however many deliveries named it: a
// delivery cuts the wait for the next cycle short, to DeliveryDelay, but
// that cycle is a whole one, with its one Kelos read and its project
// listings. The periodic reads stay authoritative: a lost delivery only
// delays what they find, on GitHub until the backstop. Every successful read of a root's source advances
// that source's LastSuccessfulRead; once there is one, a failed read
// opens a gap, from the last success to the next, with the reason, unless
// one is open already: a failed Kelos read, which serves every root,
// opens one on every root's Kelos coverage. A restart opens one on every
// source of every root that has a last success. Before a source's first
// success nothing is known of it, so there is no gap to open. A gap is
// committed as soon as it opens, or else with the cycle's end. A read the
// shutdown interrupts commits nothing.
//
// A source that is off (its configuration is incomplete, so it has no
// client) is never read: with Kelos off no root is named by runs, with
// Vikunja off no project is listed, and each root read has its coverage
// say the source was not read, and why. A gap a restart opened on it stays
// open.
//
// One process collects at a time: the loop runs under a lock, and a
// process that cannot take it reads nothing and says so. Each source read
// and each store operation has a deadline of its own, so that neither a
// hanging source nor a hanging store holds the lock while no cycle
// progresses.
package reconcile
