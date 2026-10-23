// Package kelos reads Kelos Tasks and TaskRecords (Kelos 0.60.0, API
// kelos.dev/v1alpha2) from one namespace of the Kubernetes API and turns
// them into model runs, each identified by cluster, namespace and Task UID,
// never by Task name or branch: a retry reuses both.
//
// A Task and its TaskRecord are merged by UID. The Task is cleaned up after
// its TTL; the TaskRecord keeps the terminal interval, outcome, model and
// usage for its own retention, but exists only for a Task that reported
// usage. Each run's evidence says which of the two supplied each field.
// Every time is a lifecycle time (the API server's creation time, the
// controller's start and completion times), never CPU time or the timing
// of a phase of the agent's work.
//
// A Reader lists both resources on every read. A Watcher lists them once,
// then watches both from the lists' resourceVersions, so that it sees the
// phases a Task passes through between two reads (each event becomes a
// run.observed event at the time it was received) and the deletions. It
// commits each change with its checkpoint to a Sink, the store, resumes
// from the checkpoint after a reconnect or a restart, and relists when the
// checkpoint expired, recording the span it cannot recover as a "watch
// expired" gap on the Kelos coverage.
//
// It is read-only: it only lists and watches objects, and never creates,
// updates or deletes them.
package kelos
