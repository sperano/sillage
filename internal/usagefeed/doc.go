// Package usagefeed copies the usage events of Kelos run pods from the
// telemetry store (Loki) into sillage's store, so that a cost estimate can
// be computed from what sillage holds.
//
// Each cycle reads from the feed's read-through (stored, so that a restart
// resumes) less an overlap, up to a delay before now, in windows of bounded
// size, and saves each window before it moves the read-through to its end.
// A usage event is immutable and keyed by its pod, event, stream and
// sequence, so the overlap (exporters retry and Loki accepts late entries)
// and a repeated cycle only repeat events the store keeps once. A failed
// read or save ends the cycle without moving the read-through; the next
// cycle retries. The feed observes: nothing waits on it, and its failure
// only leaves the usage out of date.
package usagefeed
