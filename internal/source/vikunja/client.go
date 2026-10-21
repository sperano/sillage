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
	// DefaultPerPage is the comment page size requested. It matches
	// Vikunja's default service.maxitemsperpage; a server configured lower
	// caps it silently, which is why fetchComments also honours
	// headerTotalPages.
	DefaultPerPage = 50
	// DefaultRequestTimeout bounds each HTTP request of the default client.
	DefaultRequestTimeout = 30 * time.Second
	// maxResponseBytes bounds the body read from any one response.
	maxResponseBytes = 16 << 20
	// maxCommentPages stops a server that never returns a short page.
	maxCommentPages = 10_000
	// headerTotalPages is Vikunja's page-count header on paginated lists.
	headerTotalPages = "X-Pagination-Total-Pages"
	// expandBuckets asks Vikunja to embed the task's Kanban buckets.
	expandBuckets = "buckets"
)

// Client reads tasks and comments from one Vikunja instance. It only sends
// GET requests.
type Client struct {
	baseURL string
	token   Token
	http    *http.Client
	perPage int
	now     func() time.Time
}

// NewClient returns a client for cfg. A nil hc gets a client with
// DefaultRequestTimeout.
func NewClient(cfg Config, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	return &Client{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		http:    hc,
		perPage: DefaultPerPage,
		now:     time.Now,
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
		Instance:   c.baseURL,
		Task:       task,
		Comments:   comments,
		ObservedAt: observed,
	}, nil
}

func (c *Client) fetchTask(ctx context.Context, taskID int64) (Task, error) {
	var task Task
	query := url.Values{"expand": {expandBuckets}}
	if _, err := c.get(ctx, taskPath(taskID), query, &task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// fetchComments follows page until one comes back shorter than perPage,
// unless the total-pages header says more remain. The result is sorted
// oldest first.
func (c *Client) fetchComments(ctx context.Context, taskID int64) ([]Comment, error) {
	var all []Comment
	for page := 1; ; page++ {
		if page > maxCommentPages {
			return nil, fmt.Errorf("vikunja: task %d: more than %d comment pages", taskID, maxCommentPages)
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
		all = append(all, batch...)
		if len(batch) < c.perPage && page >= totalPages(header) {
			break
		}
	}
	slices.SortStableFunc(all, func(a, b Comment) int {
		return cmp.Or(a.Created.Compare(b.Created), cmp.Compare(a.ID, b.ID))
	})
	return all, nil
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
// Errors name the method and path, never the token.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	target := c.baseURL + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("vikunja: GET %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+string(c.token))
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vikunja: GET %s: %w", path, unwrapURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vikunja: GET %s: %s", path, resp.Status)
	}
	body := io.LimitReader(resp.Body, maxResponseBytes)
	if err := json.NewDecoder(body).Decode(out); err != nil {
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
