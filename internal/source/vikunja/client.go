package vikunja

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

const (
	// apiPrefix is where Vikunja serves its REST API under the base URL.
	apiPrefix = "/api/v1"
	// defaultPerPage is the comment page size requested. It matches
	// Vikunja's default service.maxitemsperpage; a server configured lower
	// caps it silently, which is why fetchComments also honours
	// headerTotalPages.
	defaultPerPage = 50
	// DefaultRequestTimeout bounds each HTTP request of the default client.
	DefaultRequestTimeout = 30 * time.Second
	// defaultMaxResponseBytes bounds the body read from any one response.
	defaultMaxResponseBytes = 16 << 20
	// defaultMaxCommentPages stops a server that never returns a short page.
	defaultMaxCommentPages = 10_000
	// drainBytes is how much of an error response is read and discarded so
	// the connection can be reused.
	drainBytes = 4 << 10
	// headerTotalPages is Vikunja's page-count header on paginated lists.
	headerTotalPages = "X-Pagination-Total-Pages"
	// expandBuckets asks Vikunja to embed the task's Kanban buckets.
	expandBuckets = "buckets"
)

// StatusError is a response other than 200 OK.
type StatusError struct {
	// Path is the API path requested, without the base URL and query.
	Path string
	// Code is the HTTP status code; Status its full text ("404 Not Found").
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return "vikunja: GET " + e.Path + ": " + e.Status
}

// Client reads tasks and comments from one Vikunja instance. It only sends
// GET requests. It holds the token only inside authorize, so printing a
// Client never prints the token.
type Client struct {
	baseURL   string
	instance  string
	authorize func(*http.Request)
	http      *http.Client
	now       func() time.Time
	// perPage, maxPages and maxBody are fields so tests can shrink them.
	perPage  int
	maxPages int
	maxBody  int64
}

// NewClient returns a client for cfg. A nil hc gets a client with
// DefaultRequestTimeout.
func NewClient(cfg Config, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	header := "Bearer " + string(cfg.Token)
	return &Client{
		baseURL:   cfg.BaseURL,
		instance:  cfg.Instance,
		authorize: func(r *http.Request) { r.Header.Set("Authorization", header) },
		http:      hc,
		now:       time.Now,
		perPage:   defaultPerPage,
		maxPages:  defaultMaxCommentPages,
		maxBody:   defaultMaxResponseBytes,
	}
}

// Fetch reads task taskID and every page of its comments.
func (c *Client) Fetch(ctx context.Context, taskID int64) (Snapshot, error) {
	task, err := c.fetchTask(ctx, taskID)
	if err != nil {
		return Snapshot{}, err
	}
	observed := c.now()
	comments, err := c.fetchComments(ctx, taskID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Instance:           c.instance,
		Task:               task,
		Comments:           comments,
		ObservedAt:         observed,
		CommentsObservedAt: c.now(),
	}, nil
}

func (c *Client) fetchTask(ctx context.Context, taskID int64) (Task, error) {
	var task Task
	query := url.Values{"expand": {expandBuckets}}
	if _, err := c.get(ctx, taskPath(taskID), query, &task); err != nil {
		return Task{}, err
	}
	if task.ID != taskID {
		return Task{}, fmt.Errorf("vikunja: GET %s: answered with task %d", taskPath(taskID), task.ID)
	}
	return task, nil
}

// fetchComments follows page until one comes back shorter than perPage,
// unless the total-pages header says more remain. It also stops at a page
// with no comment it has not seen, as a server that ignores page sends. The
// result is sorted oldest first, each comment once.
func (c *Client) fetchComments(ctx context.Context, taskID int64) ([]Comment, error) {
	var all []Comment
	seen := make(map[int64]bool)
	for page := 1; ; page++ {
		if page > c.maxPages {
			return nil, fmt.Errorf("vikunja: task %d: more than %d comment pages", taskID, c.maxPages)
		}
		var batch []Comment
		query := url.Values{
			"page":     {strconv.Itoa(page)},
			"per_page": {strconv.Itoa(c.perPage)},
		}
		header, err := c.get(ctx, taskPath(taskID)+"/comments", query, &batch)
		if err != nil {
			return nil, err
		}
		before := len(all)
		all = appendUnseen(all, batch, seen)
		if len(batch) > 0 && len(all) == before {
			break
		}
		if len(batch) < c.perPage && page >= totalPages(header) {
			break
		}
	}
	slices.SortStableFunc(all, func(a, b Comment) int {
		return cmp.Or(a.Created.Compare(b.Created), cmp.Compare(a.ID, b.ID))
	})
	return all, nil
}

// appendUnseen appends the comments of batch whose id is not in seen, and
// marks them seen. A comment posted between two page reads can repeat one.
func appendUnseen(all, batch []Comment, seen map[int64]bool) []Comment {
	for _, c := range batch {
		if !seen[c.ID] {
			seen[c.ID] = true
			all = append(all, c)
		}
	}
	return all
}

// totalPages reads headerTotalPages; 0 when it is missing or malformed.
func totalPages(h http.Header) int {
	n, err := strconv.Atoi(h.Get(headerTotalPages))
	if err != nil {
		return 0
	}
	return n
}

func taskPath(taskID int64) string {
	return apiPrefix + "/tasks/" + strconv.FormatInt(taskID, 10)
}

// get decodes the JSON at path into out and returns the response headers.
// Errors name the method and path, never the token or the full URL.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	target := c.baseURL + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("vikunja: GET %s: %w", path, err)
	}
	c.authorize(req)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vikunja: GET %s: %w", path, unwrapURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		return nil, &StatusError{Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	body := http.MaxBytesReader(nil, resp.Body, c.maxBody)
	if err := json.NewDecoder(body).Decode(out); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, fmt.Errorf("vikunja: GET %s: response larger than %d bytes", path, c.maxBody)
		}
		return nil, fmt.Errorf("vikunja: GET %s: decoding: %w", path, err)
	}
	return resp.Header, nil
}

// unwrapURLError drops *url.Error's copy of the request URL, so an error
// never repeats the full target; get already names the path.
func unwrapURLError(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}
