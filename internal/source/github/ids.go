package github

import "strconv"

// The prefixes of the ids of the events made of a pull request's
// conversation comments, inline review comments and reviews, followed by
// GitHub's id of each: those ids are unique per GitHub instance.
const (
	commentIDPrefix       = "issues/comments/"
	reviewCommentIDPrefix = "pulls/comments/"
	reviewIDPrefix        = "reviews/"
)

// CommentEventID is the id of the event made of conversation comment id.
func CommentEventID(id int64) string { return commentIDPrefix + strconv.FormatInt(id, 10) }

// ReviewCommentEventID is the id of the event made of inline review
// comment id.
func ReviewCommentEventID(id int64) string { return reviewCommentIDPrefix + strconv.FormatInt(id, 10) }

// ReviewEventID is the id of the event made of submitted review id.
func ReviewEventID(id int64) string { return reviewIDPrefix + strconv.FormatInt(id, 10) }
