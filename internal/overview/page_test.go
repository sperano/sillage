package overview

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
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
		t.Run(c.name, func(t *testing.T) {
			if got := taskIDs(buildAll(t, afternoon, c.filter)); !slices.Equal(got, c.want) {
				t.Errorf("rows = %v, want %v", got, c.want)
			}
		})
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

// A cursor names a place in the order of its page's window: rows move as
// the window does, so another window's cursor is refused.
func TestPaginateRejectsCursorOfAnotherWindow(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{})
	page, err := Paginate(afternoon, rows, Filter{}, "", 1)
	if err != nil || page.Next == "" {
		t.Fatalf("first page: next %q, error %v", page.Next, err)
	}
	later := Window{From: afternoon.From.Add(time.Second), To: afternoon.To.Add(time.Second)}
	if _, err := Paginate(later, rows, Filter{}, page.Next, 1); !errors.Is(err, ErrCursorWindow) {
		t.Errorf("error %v, want ErrCursorWindow", err)
	}
}

// A page size past the rows left after a cursor is the rest of them.
func TestPaginateLargestLimit(t *testing.T) {
	rows := buildAll(t, afternoon, Filter{})
	first, err := Paginate(afternoon, rows, Filter{}, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	rest, err := Paginate(afternoon, rows, Filter{}, first.Next, math.MaxInt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Rows) != len(rows)-1 || rest.Next != "" {
		t.Errorf("rest = %d rows, next %q; want the %d others, no next", len(rest.Rows), rest.Next, len(rows)-1)
	}
}

func TestPaginateRejectsEmptyPage(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if _, err := Paginate(afternoon, nil, Filter{}, "", limit); !errors.Is(err, ErrLimit) {
			t.Errorf("limit %d: error %v, want ErrLimit", limit, err)
		}
	}
}

// A window with no row is an empty list, never null: the web decoder
// requires a list.
func TestEmptyWindowIsAnEmptyList(t *testing.T) {
	night := Window{From: afternoon.From.Add(-12 * time.Hour), To: afternoon.From.Add(-11 * time.Hour)}
	rows, err := Build(night, loadInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	page, err := Paginate(night, rows, Filter{}, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"rows":[]`) {
		t.Errorf("page = %s, want an empty rows list", data)
	}
}
