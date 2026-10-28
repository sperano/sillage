// Package reconcile keeps the store current: a loop that reads the
// sources periodically, assembles the story of every root they name
// through the collector, and commits what changed, with how fresh each
// story's sources are and where their records are missing.
//
// Each cycle reads the Kelos namespace once and finds the roots its runs
// name, lists the configured Vikunja projects' tasks changed since the
// last listing, and collects every root that is new, whose runs changed,
// whose task changed, whose Kelos gap is open, or that was not read for a
// refresh interval. The roots webhook deliveries name (Deliveries) are
// read too, first, at most DeliveryRootsPerCycle a cycle, the others left
// queued for the next, and each once however many deliveries named it: a
// delivery cuts the wait for the next cycle short, to DeliveryDelay, but
// that cycle is a whole one, with its one Kelos read and its project
// listings. The periodic reads stay authoritative: a lost delivery only
// delays what they find. Every successful read of a root's source advances
// that source's LastSuccessfulRead; once there is one, a failed read
// opens a gap, from the last success to the next, with the reason, unless
// one is open already: a failed Kelos read, which serves every root,
// opens one on every root's Kelos coverage. A restart opens one on every
// source of every root that has a last success. Before a source's first
// success nothing is known of it, so there is no gap to open. A gap is
// committed as soon as it opens, or else with the cycle's end. A read the
// shutdown interrupts commits nothing.
//
// One process collects at a time: the loop runs under a lock, and a
// process that cannot take it reads nothing and says so. Each source read
// and each store operation has a deadline of its own, so that neither a
// hanging source nor a hanging store holds the lock while no cycle
// progresses.
package reconcile
