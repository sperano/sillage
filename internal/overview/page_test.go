package overview

import (
	"errors"
	"slices"
	"testing"
)

func TestFilters(t *testing.T) {
	for _, c := range []struct {
		name   string
		filter Filter
		want   []int64
	}{
		{"none", Filter{}, []int64{taskA, taskB, taskC, taskD, taskF, taskE}},
		{"project", Filter{Project: "gadgets"}, []int64{taskC}},
		{"title, any case", Filter{Task: "CACHE"}, []int64{taskC}},
		{"key", Filter{Task: "tasks/204"}, []int64{taskD}},
		{"executor", Filter{Executor: "agent-cedar"}, []int64{taskA, taskC}},
		{"review requested", Filter{ReviewRequested: true}, []int64{taskA, taskB}},
		{"failed run", Filter{FailedRun: true}, []int64{taskC}},
		{"missing history", Filter{MissingHistory: true}, []int64{taskE}},
		{"all must match", Filter{Executor: "agent-cedar", ReviewRequested: true}, []int64{taskA}},
		{"no match", Filter{Project: "unknown-project"}, []int64{}},
	} {
		if got := taskIDs(buildAll(t, afternoon, c.filter)); !slices.Equal(got, c.want) {
			t.Errorf("%s: rows = %v, want %v", c.name, got, c.want)
		}
	}
}

// Following Next from the first page visits every row once, in order.
func TestPaginateFollowsNext(t *testing.T) {
	rows, err := Build(afternoon, loadInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	const limit = 2
	var seen []int64
	after := ""
	for range len(rows) {
		page, err := Paginate(afternoon, rows, Filter{}, after, limit)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != len(rows) || len(page.Rows) > limit {
			t.Fatalf("page of %d rows, total %d; want at most %d, total %d", len(page.Rows), page.Total, limit, len(rows))
		}
		seen = append(seen, taskIDs(page.Rows)...)
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if want := taskIDs(buildAll(t, afternoon, Filter{})); !slices.Equal(seen, want) {
		t.Errorf("pages = %v, want %v", seen, want)
	}
}

func TestPaginateRejectsForeignCursor(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{})
	for _, cursor := range []string{"not base64!", "e30", "eyJrZXkiOiJ4In0"} {
		if _, err := Paginate(afternoon, rows, Filter{}, cursor, 1); !errors.Is(err, ErrCursor) {
			t.Errorf("cursor %q: error %v, want ErrCursor", cursor, err)
		}
	}
}
