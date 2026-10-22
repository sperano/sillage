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
// It is read-only: it only lists objects, and never creates, updates or
// deletes them.
package kelos
