package vikunja

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// allTasksPath lists the tasks of every project the token can read,
// filtered with Vikunja's filter syntax.
const allTasksPath = apiPrefix + "/tasks/all"

// TaskChange is a task as a listing names it: its id, and when it last
// changed.
type TaskChange struct {
	ID      int64     `json:"id"`
	Updated time.Time `json:"updated"`
}

// ChangedTasks lists the tasks of project whose updated time is at or
// after since (every task of the project when since is zero), in id
// order, each once. It follows every page: Vikunja caps per_page
// silently, so it also honours the total-pages header. Pages are in id
// order because an id never changes: a task updated while the pages are
// read keeps its place, where in updated order it would move to the end
// and push another out of the page it was on.
func (c *Client) ChangedTasks(ctx context.Context, project int64, since time.Time) ([]TaskChange, error) {
	filter := "project = " + strconv.FormatInt(project, 10)
	if !since.IsZero() {
		filter += " && updated >= " + since.UTC().Format(time.RFC3339)
	}
	var all []TaskChange
	seen := make(map[int64]bool)
	for page := 1; ; page++ {
		if page > c.maxPages {
			return nil, fmt.Errorf("vikunja: project %d: more than %d task pages", project, c.maxPages)
		}
		var batch []TaskChange
		query := url.Values{
			"filter":   {filter},
			"sort_by":  {"id"},
			"order_by": {"asc"},
			"page":     {strconv.Itoa(page)},
			"per_page": {strconv.Itoa(c.perPage)},
		}
		header, err := c.get(ctx, allTasksPath, query, &batch)
		if err != nil {
			return nil, err
		}
		before := len(all)
		for _, t := range batch {
			if !seen[t.ID] {
				seen[t.ID] = true
				all = append(all, t)
			}
		}
		if lastPage(len(batch), len(all) == before, page, c.perPage, totalPages(header)) {
			break
		}
	}
	slices.SortFunc(all, func(a, b TaskChange) int { return cmp.Compare(a.ID, b.ID) })
	return all, nil
}

// lastPage reports whether page, which held n tasks, the first one seen
// already when repeated, is the listing's last: it is empty or repeats
// one before; else it reaches a total-pages header, when one is set (a
// page can be full and last); else it is short.
func lastPage(n int, repeated bool, page, perPage, total int) bool {
	switch {
	case n == 0 || repeated:
		return true
	case total > 0:
		return page >= total
	}
	return n < perPage
}
