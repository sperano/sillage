package github

import "testing"

func TestEventIDs(t *testing.T) {
	for got, want := range map[string]string{
		CommentEventID(11):       "issues/comments/11",
		ReviewCommentEventID(12): "pulls/comments/12",
		ReviewEventID(13):        "reviews/13",
	} {
		if got != want {
			t.Errorf("event id %q, want %q", got, want)
		}
	}
}
