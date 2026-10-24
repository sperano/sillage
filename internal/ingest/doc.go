// Package ingest receives Vikunja and GitHub webhook deliveries, records
// each attempt as evidence that sillage received it, and asks the
// collector to read again the roots it names.
//
// A delivery only says that something changed: no record is written from
// its payload. The roots it names are read again through the collector,
// so the story keeps one code path and fresh data, and deliveries that
// arrive late, twice or out of order change nothing a read would not.
// The periodic reconciliation stays authoritative: GitHub never retries a
// failed delivery and Vikunja sends none for a label change, so a missed
// delivery only delays what the next periodic read finds.
//
// The ingest paths are reached without the browser's Authentik
// forward-auth, so the signature is their only authentication: a source
// without a secret (EnvGitHubSecret, EnvVikunjaSecret) refuses every
// delivery. A request that declares a body too large is refused unread;
// one whose body is too large, unsigned or signed with another secret is
// refused once its body, read up to the limit, is checked, and nothing of
// it is recorded or acted on. Only a few bodies are read at once, so that
// unauthenticated requests cannot fill the memory. An authenticated
// attempt is recorded with its outcome (model.Delivery), apart from every
// story's records.
//
// Attempts of a delivery accepted already are ignored, so that a
// redelivery asks for no second read: GitHub's delivery id is its
// X-GitHub-Delivery header; Vikunja sends none, so the SHA-256 of the
// signed body, which holds the time Vikunja sent it, stands in, and a
// Vikunja body replayed unchanged is ignored too. That includes a
// redelivery an operator asks GitHub for: what it would have read again,
// the next periodic read does. An accepted attempt is recorded in the
// store transaction that schedules its reads, so that of two attempts of
// one delivery racing, only one schedules them. An attempt that failed is
// not accepted, and a retry of it is read, though a failed attempt can
// have scheduled some of its reads before the queue filled.
//
// GitHub signs the body but not its headers, so a captured body replayed
// under another delivery id is accepted again: harmless, as it only asks
// for a read of fresh data, which the queue coalesces.
package ingest
