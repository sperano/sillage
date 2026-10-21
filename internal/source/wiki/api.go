package wiki

import "time"

// The types below decode Wiki.js 2's GraphQL replies. They keep only the
// fields sillage reads.

// Page is the live page, from pages.single. Its author and creator fields
// need write:pages, so they are zero when the page was read through
// pages.list instead (see Snapshot.Restricted).
type Page struct {
	ID     int64  `json:"id"`
	Path   string `json:"path"`
	Locale string `json:"locale"`
	// CreatedAt is when the page was saved first.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is the last write to the page: the newest save's render,
	// seconds after the save, or a later move or re-render.
	UpdatedAt time.Time `json:"updatedAt"`
	// AuthorID and AuthorName name who saved the current content. A move
	// does not change them.
	AuthorID    int64  `json:"authorId"`
	AuthorName  string `json:"authorName"`
	CreatorID   int64  `json:"creatorId"`
	CreatorName string `json:"creatorName"`
}

// Version is one entry of pages.history's trail: a state of the page that
// a later save archived. VersionDate and the author are those of the save
// (or move) that produced the state, not of the save that archived it.
type Version struct {
	VersionID   int64     `json:"versionId"`
	VersionDate time.Time `json:"versionDate"`
	AuthorID    int64     `json:"authorId"`
	AuthorName  string    `json:"authorName"`
	// ActionType is computed by Wiki.js from consecutive entries:
	// "initial", "edit", or "move" with the old and new path in
	// ValueBefore and ValueAfter.
	ActionType  string  `json:"actionType"`
	ValueBefore *string `json:"valueBefore"`
	ValueAfter  *string `json:"valueAfter"`
}

// The trail's action types sillage reads.
const actionMove = "move"

// Comment is a page comment from comments.list. Render is its HTML: the
// Markdown source needs write:comments. Its updatedAt is not read: it
// differs from createdAt by a millisecond on some unedited comments, and
// an edit's earlier text is not kept.
type Comment struct {
	ID         int64     `json:"id"`
	Render     string    `json:"render"`
	AuthorID   int64     `json:"authorId"`
	AuthorName string    `json:"authorName"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Snapshot is one read of a page: the live page, its whole history trail
// and its comments.
type Snapshot struct {
	// Instance names the Wiki.js instance (see Config.Instance).
	Instance string
	Page     Page
	// Restricted reports that the token may not read pages.single, so the
	// live page came from pages.list: its author and creator are unknown.
	Restricted bool
	// Trail is the history trail, oldest first, each version once.
	Trail []Version
	// NewestPath is the path of the newest trail version; empty when the
	// trail is. A live path that differs means the last write moved the
	// page.
	NewestPath string
	// Comments are oldest first.
	Comments []Comment
	// ObservedAt is when the last of these was read.
	ObservedAt time.Time
}
