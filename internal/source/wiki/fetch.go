package wiki

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
)

const (
	// defaultHistoryPageSize is the trail page size requested: Wiki.js's
	// own default.
	defaultHistoryPageSize = 100
	// defaultMaxHistoryPages stops a trail whose total is never reached.
	defaultMaxHistoryPages = 1000
)

// The operations the client sends. Each is a query: the client never
// sends a mutation.
const (
	opPage = "SillagePage"
	// Every field but id needs write:pages, and the query itself
	// manage:pages or delete:pages on the page.
	queryPage = `query SillagePage($id: Int!) { pages { single(id: $id) {
		id path locale createdAt updatedAt authorId authorName creatorId creatorName } } }`

	opPageList = "SillagePageList"
	// pages.list needs only read:pages, but names no author.
	queryPageList = `query SillagePageList { pages { list { id path locale createdAt updatedAt } } }`

	opHistory    = "SillageHistory"
	queryHistory = `query SillageHistory($id: Int!, $offsetPage: Int!, $offsetSize: Int!) { pages {
		history(id: $id, offsetPage: $offsetPage, offsetSize: $offsetSize) {
			trail { versionId versionDate authorId authorName actionType valueBefore valueAfter } total } } }`

	opVersion    = "SillageVersion"
	queryVersion = `query SillageVersion($pageId: Int!, $versionId: Int!) { pages {
		version(pageId: $pageId, versionId: $versionId) { path } } }`

	opComments    = "SillageComments"
	queryComments = `query SillageComments($locale: String!, $path: String!) { comments {
		list(locale: $locale, path: $path) { id render authorId authorName createdAt } } }`
)

// Fetch reads page pageID: the live page, every page of its history trail,
// the newest trail version's path, and its comments.
//
// The live page is read first: a save during the read then shows up as a
// trail version the live page already showed, never as a version missing
// between the trail and the live page. A token that may not read
// pages.single (it needs manage:pages or delete:pages, and the author
// fields write:pages) falls back to pages.list, which needs only
// read:pages but names neither the live version's author nor the
// creator. The trail needs read:history, the comments read:comments.
func (c *Client) Fetch(ctx context.Context, pageID int64) (Snapshot, error) {
	page, restricted, err := c.fetchPage(ctx, pageID)
	if err != nil {
		return Snapshot{}, err
	}
	trail, err := c.fetchTrail(ctx, pageID)
	if err != nil {
		return Snapshot{}, err
	}
	var newestPath string
	if len(trail) > 0 {
		if newestPath, err = c.fetchVersionPath(ctx, pageID, trail[len(trail)-1].VersionID); err != nil {
			return Snapshot{}, err
		}
	}
	comments, err := c.fetchComments(ctx, page)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Instance:   c.instance,
		Page:       page,
		Restricted: restricted,
		Trail:      trail,
		NewestPath: newestPath,
		Comments:   comments,
		ObservedAt: c.now(),
	}, nil
}

// fetchPage reads the live page through pages.single, or through
// pages.list when the token may not read pages.single; restricted
// reports the latter.
func (c *Client) fetchPage(ctx context.Context, pageID int64) (page Page, restricted bool, err error) {
	var data struct {
		Pages struct {
			Single *Page `json:"single"`
		} `json:"pages"`
	}
	err = c.query(ctx, opPage, queryPage, map[string]any{"id": pageID}, &data)
	if ge, ok := errors.AsType[*GraphQLError](err); ok && ge.forbidden() {
		page, err = c.listPage(ctx, pageID)
		return page, true, err
	}
	if err != nil {
		return Page{}, false, err
	}
	if data.Pages.Single == nil || data.Pages.Single.ID != pageID {
		return Page{}, false, fmt.Errorf("wiki: %s: no page %d in the reply", opPage, pageID)
	}
	return *data.Pages.Single, false, nil
}

// listPage finds page pageID in pages.list, which has no id filter.
func (c *Client) listPage(ctx context.Context, pageID int64) (Page, error) {
	var data struct {
		Pages struct {
			List []Page `json:"list"`
		} `json:"pages"`
	}
	if err := c.query(ctx, opPageList, queryPageList, nil, &data); err != nil {
		return Page{}, err
	}
	i := slices.IndexFunc(data.Pages.List, func(p Page) bool { return p.ID == pageID })
	if i < 0 {
		return Page{}, fmt.Errorf("wiki: %s: page %d is not listed", opPageList, pageID)
	}
	return data.Pages.List[i], nil
}

// fetchTrail reads the trail page by page until it holds the reported
// total. The trail is newest first, so a save during the read shifts
// older versions to later pages: one can repeat, none is skipped. The
// result is oldest first, each version once.
func (c *Client) fetchTrail(ctx context.Context, pageID int64) ([]Version, error) {
	var all []Version
	seen := make(map[int64]bool)
	for page := 0; ; page++ {
		if page >= c.maxHistoryPages {
			return nil, fmt.Errorf("wiki: page %d: more than %d history pages", pageID, c.maxHistoryPages)
		}
		batch, total, err := c.fetchTrailPage(ctx, pageID, page)
		if err != nil {
			return nil, err
		}
		for _, v := range batch {
			if !seen[v.VersionID] {
				seen[v.VersionID] = true
				all = append(all, v)
			}
		}
		if len(batch) == 0 || len(all) >= total {
			break
		}
	}
	slices.SortStableFunc(all, func(a, b Version) int {
		return cmp.Or(a.VersionDate.Compare(b.VersionDate), cmp.Compare(a.VersionID, b.VersionID))
	})
	return all, nil
}

func (c *Client) fetchTrailPage(ctx context.Context, pageID int64, page int) ([]Version, int, error) {
	var data struct {
		Pages struct {
			History *struct {
				Trail []Version `json:"trail"`
				Total int       `json:"total"`
			} `json:"history"`
		} `json:"pages"`
	}
	vars := map[string]any{"id": pageID, "offsetPage": page, "offsetSize": c.historyPageSize}
	if err := c.query(ctx, opHistory, queryHistory, vars, &data); err != nil {
		return nil, 0, err
	}
	h := data.Pages.History
	if h == nil {
		return nil, 0, fmt.Errorf("wiki: %s: no history for page %d in the reply", opHistory, pageID)
	}
	return h.Trail, h.Total, nil
}

// fetchVersionPath reads the path of one trail version: the trail itself
// names a path only on a move.
func (c *Client) fetchVersionPath(ctx context.Context, pageID, versionID int64) (string, error) {
	var data struct {
		Pages struct {
			Version *struct {
				Path string `json:"path"`
			} `json:"version"`
		} `json:"pages"`
	}
	vars := map[string]any{"pageId": pageID, "versionId": versionID}
	if err := c.query(ctx, opVersion, queryVersion, vars, &data); err != nil {
		return "", err
	}
	if v := data.Pages.Version; v != nil && v.Path != "" {
		return v.Path, nil
	}
	return "", fmt.Errorf("wiki: %s: no path for version %d of page %d", opVersion, versionID, pageID)
}

// fetchComments reads the page's comments, oldest first. Wiki.js finds
// them by locale and path and answers an empty list for a path with no
// page, so a page moved during the read loses its comments for this read.
func (c *Client) fetchComments(ctx context.Context, page Page) ([]Comment, error) {
	var data struct {
		Comments struct {
			List []Comment `json:"list"`
		} `json:"comments"`
	}
	vars := map[string]any{"locale": page.Locale, "path": page.Path}
	if err := c.query(ctx, opComments, queryComments, vars, &data); err != nil {
		return nil, err
	}
	comments := data.Comments.List
	slices.SortStableFunc(comments, func(a, b Comment) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return comments, nil
}
