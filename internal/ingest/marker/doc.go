// Package marker serves POST /ingest/marker: the phase markers that the
// wrappers in a Kelos run's pod (wait-for-blockers, the wiki login, the
// agent's entrypoint) report, one edge of one span per request.
//
// The path is reached on the in-cluster Service only: the public tunnel
// forwards no marker path, and the public host keeps its forward-auth on
// every path. Each request is authenticated by the pod's projected
// ServiceAccount token, audience Audience, which a TokenReview checks: the
// pod the review names is the marker's sender, never anything the body
// says. Only the ServiceAccounts of the Kelos namespace are accepted, with a
// pod name and UID. A verdict is cached by the token's SHA-256 until the
// token expires, at most maxVerdictLifetime, so that most of a run's markers
// cost no review (a full cache, or markers arriving together before their
// token's first verdict, cost one each); a token refused is never cached, as
// nothing vouches for the expiry it claims. Only a few reviews run at once,
// and only a few bodies are read at once.
//
// The body is one model.MarkerReport, wire format version 1, at most
// MaxBodyBytes, decoded strictly: an unknown field, a value outside its
// closed list, a time not in UTC or more than MaxSkew away from its receipt
// is refused. The answers:
//
//   - 202: stored, and joined to its run if the store knows it.
//   - 200: an identical re-send of a stored marker.
//   - 409: a different marker under the same pod UID, span and edge; the
//     first is kept.
//   - 429: the pod has PerPodLimit markers; the coverage of its run says
//     so.
//   - 400: an invalid body; 413: a body too large.
//   - 401: a token missing or refused.
//   - 503: the TokenReview or the store failed, or too many reviews or
//     bodies were in flight for a second.
//
// The emitter ignores the answer, and never retries. Like the webhook
// ingestion, a marker is evidence that sillage received it: a lost one is
// lost, and nothing reads it again.
package marker
