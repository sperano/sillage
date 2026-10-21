// Package wiki reads a Wiki.js 2 page's versions (the live version
// included, which the history trail omits) and comments through GraphQL,
// and turns them into model events: the page's creation, edits and moves,
// /kelos requests, start announcements and replies. It is read-only: it
// sends queries, never mutations.
//
// It records only what Wiki.js records. Comment edits leave no trace of
// the earlier text, so none is claimed; Wiki.js keeps no read log, so no
// reading time is ever measured or estimated. It does not query GitHub:
// Branch names the branch that links a page to its pull requests.
package wiki
