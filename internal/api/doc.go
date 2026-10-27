// Package api serves sillage's private, read-only JSON API over the store,
// under /api/v1/: the story of a task (model.Story as JSON,
// unchanged: the golden stories under testdata/fixtures/stories are its
// contract), a root's coverage, how fresh each source's reads are, root
// search, the resolution of a pasted link to the story it belongs to, the
// multi-task overview of a window (internal/overview's rows, paginated:
// the golden overview under testdata/fixtures/overview is its contract),
// the binary's version, and the frontend's configuration (the Kelos
// console's base URL per cluster, for run links).
//
// The API has no authentication of its own. Authentik forward-auth in
// front of sillage (Hollingsworth's helm/sillage gateway-auth) is what
// keeps it private: every path under /api/ must stay behind it. Nothing
// here writes, and no endpoint takes a request body.
//
// Every response, an error's too, is JSON with Cache-Control: no-store. An
// error is a small problem body (RFC 9457) carrying the status. A coverage
// gap is data, never an error: a story with gaps is a 200. Each request is
// bounded in time (Config.Timeout) and each response in size
// (Config.MaxResponseBytes); a response past the bound is an error, never
// a truncated story.
package api
