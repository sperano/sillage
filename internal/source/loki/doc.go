// Package loki reads the usage events Kelos run pods export as OpenTelemetry
// logs (Claude Code's api_request and api_error, Codex's response.completed,
// api_request and turn_cost) back from Loki's HTTP API, and normalizes them
// into model.UsageEvent. It only ever sends GET requests.
//
// Alloy keeps service_name and k8s_namespace_name as stream labels and
// everything else as structured metadata, whose dots are underscores:
// Query's answer merges the two whichever response encoding Loki uses. A
// pod's identity is what the collector stamped, and an event without
// one (pod UID, pod name, stream, sequence) is counted as malformed, never
// given an invented one. A counter an event does not carry stays nil
// (unknown), never zero.
package loki
