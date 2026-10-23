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
// delivery, and a request whose body is too large, unsigned or signed
// with another secret is refused before anything is read from it or
// recorded. An authenticated attempt is recorded with its outcome
// (model.Delivery), apart from every story's records. Attempts of a
// delivery accepted already are ignored, so that a redelivery or a
// replayed body asks for no second read: GitHub's delivery id is its
// X-GitHub-Delivery header; Vikunja sends none, so the SHA-256 of the
// signed body, which holds the time Vikunja sent it, stands in.
package ingest
