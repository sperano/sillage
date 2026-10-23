package model

import (
	"maps"
	"testing"
)

func TestRecordsRefs(t *testing.T) {
	page := testPageKey()
	onPage := testEvent("on-page")
	onPage.Related = Related{Page: &page}
	recs := Records{
		Events:       []Event{testEvent("c-1"), onPage},
		Runs:         []Run{testRun("uid-1")},
		PullRequests: []PullRequest{testPullRequest()},
	}
	root := Root{Task: &TaskKey{Instance: testVikunjaHost, ID: testTaskID + 1}}
	want := map[Ref]bool{
		root.Task.Ref(): true, testTaskKey().Ref(): true, page.Ref(): true,
		recs.Events[0].Ref(): true, onPage.Ref(): true,
		testRunKey("uid-1").Ref(): true, testPullRequest().Key.Ref(): true,
	}
	if got := recs.Refs(root); !maps.Equal(got, want) {
		t.Errorf("Refs() = %v, want %v", got, want)
	}
}
