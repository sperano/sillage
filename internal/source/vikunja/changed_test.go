package vikunja

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"
)

// changedServer answers task listings from pages, by page number, and
// records the filters it was sent.
func changedServer(t *testing.T, pages map[int][]TaskChange, totalPages int, filters *[]string) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+allTasksPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("sort_by") != "id" || q.Get("order_by") != "asc" {
			t.Errorf("sorted by %q %q, want id asc", q.Get("sort_by"), q.Get("order_by"))
		}
		*filters = append(*filters, q.Get("filter"))
		page, _ := strconv.Atoi(q.Get("page"))
		if totalPages > 0 {
			w.Header().Set(headerTotalPages, strconv.Itoa(totalPages))
		}
		_ = json.NewEncoder(w).Encode(pages[page])
	})
	return mux
}

func changes(ids ...int64) []TaskChange {
	out := make([]TaskChange, len(ids))
	for i, id := range ids {
		out[i] = TaskChange{ID: id, Updated: testObservedAt.Add(time.Duration(id) * time.Minute)}
	}
	return out
}

func changedIDs(cs []TaskChange) []int64 {
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

func TestChangedTasksFilters(t *testing.T) {
	since := time.Date(2026, 3, 6, 7, 30, 0, 0, time.FixedZone("EST", -5*60*60))
	tests := []struct {
		name  string
		since time.Time
		want  string
	}{
		{"every task", time.Time{}, "project = 9"},
		{"since", since, "project = 9 && updated >= 2026-03-06T12:30:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filters []string
			c := newTestClient(t, changedServer(t, map[int][]TaskChange{1: changes(1)}, 0, &filters))
			if _, err := c.ChangedTasks(t.Context(), 9, tt.since); err != nil {
				t.Fatal(err)
			}
			if len(filters) != 1 || filters[0] != tt.want {
				t.Errorf("filters = %q, want [%q]", filters, tt.want)
			}
		})
	}
}

func TestChangedTasksPagination(t *testing.T) {
	tests := []struct {
		name       string
		pages      map[int][]TaskChange
		totalPages int
		want       []int64
		requests   int
	}{
		{"short last page", map[int][]TaskChange{1: changes(1, 2), 2: changes(3)}, 0, []int64{1, 2, 3}, 2},
		{"full last page", map[int][]TaskChange{1: changes(1, 2), 2: changes(3, 4)}, 0, []int64{1, 2, 3, 4}, 3},
		// A server capping per_page below the page size asked for.
		{"capped page size", map[int][]TaskChange{1: changes(1), 2: changes(2), 3: changes(3)}, 3, []int64{1, 2, 3}, 3},
		{"full last page, total pages", map[int][]TaskChange{1: changes(1, 2), 2: changes(3, 4)}, 2, []int64{1, 2, 3, 4}, 2},
		{"repeated page", map[int][]TaskChange{1: changes(1, 2), 2: changes(1, 2)}, 0, []int64{1, 2}, 2},
		{"duplicate across pages", map[int][]TaskChange{1: changes(2, 1), 2: changes(1)}, 0, []int64{1, 2}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filters []string
			c := newTestClient(t, changedServer(t, tt.pages, tt.totalPages, &filters))
			got, err := c.ChangedTasks(t.Context(), 9, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(changedIDs(got), tt.want) || len(filters) != tt.requests {
				t.Errorf("ChangedTasks() = %v in %d requests, want %v in %d", changedIDs(got), len(filters), tt.want,
					tt.requests)
			}
		})
	}
}

func TestChangedTasksStopsAtMaxPages(t *testing.T) {
	var filters []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		filters = append(filters, r.URL.Query().Get("filter"))
		_ = json.NewEncoder(w).Encode(changes(int64(2*page), int64(2*page+1)))
	})
	c := newTestClient(t, h)
	c.maxPages = 3
	if _, err := c.ChangedTasks(t.Context(), 9, time.Time{}); err == nil {
		t.Error("ChangedTasks() error = nil, want the endless listing refused")
	}
	if len(filters) != c.maxPages {
		t.Errorf("%d requests, want %d", len(filters), c.maxPages)
	}
}

func TestChangedTasksStatusError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := newTestClient(t, h).ChangedTasks(t.Context(), 9, time.Time{})
	if se, ok := errors.AsType[*StatusError](err); !ok || se.Code != http.StatusServiceUnavailable || se.Path != allTasksPath {
		t.Errorf("ChangedTasks() error = %v, want a 503 StatusError on %s", err, allTasksPath)
	}
}
