package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// mediaType is the Accept header GitHub recommends for its REST API.
	mediaType = "application/vnd.github+json"
	// headerAPIVersion pins the REST API version the types in api.go
	// decode.
	headerAPIVersion = "X-GitHub-Api-Version"
	apiVersion       = "2022-11-28"
	// userAgent identifies sillage; GitHub refuses requests without one.
	userAgent = "sillage"
	// headerLink carries the URLs of a paginated list's other pages.
	headerLink = "Link"
	// defaultPerPage is the page size requested: GitHub's maximum.
	defaultPerPage = 100
	// DefaultRequestTimeout bounds each HTTP request of the default client.
	DefaultRequestTimeout = 30 * time.Second
	// defaultMaxResponseBytes bounds the body read from any one response.
	defaultMaxResponseBytes = 16 << 20
	// defaultMaxPages stops a list whose Link header never ends.
	defaultMaxPages = 1000
	// drainBytes is how much of an error response is read, to tell a rate
	// limit from another refusal, and how much of any response's tail is
	// discarded, so the connection can be reused.
	drainBytes = 4 << 10
	// checkRunsRefusalTTL is how long a repository whose check runs were
	// refused (403) is not asked for them again: the token or App
	// lacks Checks read, which does not change from one read to the next.
	checkRunsRefusalTTL = 24 * time.Hour
)

// StatusError is a response other than 200 OK that is not a rate limit.
type StatusError struct {
	// Path is the API path requested, without the base URL and query.
	Path string
	// Code is the HTTP status code; Status its full text ("404 Not Found").
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return "github: GET " + e.Path + ": " + e.Status
}

// Client reads pull requests from one GitHub instance. It only sends GET
// requests to the API it reads; the one POST is the mint of a GitHub App
// installation token, when the Config has an App. It holds the credential
// only inside authorize, so printing a Client never prints it. It is safe
// for concurrent use.
type Client struct {
	apiURL   *url.URL
	instance string
	// authorize sets the credential of a request; it fails when an
	// installation token cannot be minted.
	authorize func(context.Context, *http.Request) error
	http      *http.Client
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
	// The fields below are fields so tests can shrink them.
	perPage    int
	maxPages   int
	maxBody    int64
	maxWait    time.Duration
	maxRetries int

	// requests counts the requests sent, and notModified those answered
	// 304 Not Modified.
	requests, notModified atomic.Int64

	mu sync.Mutex
	// resumeAt is when the rate-limit window, last seen exhausted, resets.
	resumeAt time.Time
	// budget is the rate limit the last response reported.
	budget Budget
	// checkRunsRefused holds, per repository (lower-cased owner/name), the
	// refusal of its check runs that is remembered.
	checkRunsRefused map[string]checkRunsRefusal
}

// NewClient returns a client for cfg, which must be valid (ConfigFromEnv
// returns only valid ones). A nil hc gets a client with
// DefaultRequestTimeout.
func NewClient(cfg Config, hc *http.Client) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Instance == "" {
		return nil, errors.New("github: an instance name is required")
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	u, _ := url.Parse(strings.TrimRight(cfg.APIURL, "/"))
	c := &Client{
		apiURL:     u,
		instance:   cfg.Instance,
		http:       hc,
		now:        time.Now,
		sleep:      sleepContext,
		perPage:    defaultPerPage,
		maxPages:   defaultMaxPages,
		maxBody:    defaultMaxResponseBytes,
		maxWait:    DefaultMaxRateLimitWait,
		maxRetries: defaultMaxRateLimitRetries,
	}
	if err := c.setCredential(cfg); err != nil {
		return nil, err
	}
	return c, nil
}

// setCredential makes c authenticate with cfg's token, or as its App's
// installation.
func (c *Client) setCredential(cfg Config) error {
	if cfg.App == nil {
		header := "Bearer " + string(cfg.Token)
		c.authorize = func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", header)
			return nil
		}
		return nil
	}
	tokens, err := newInstallationTokens(c.apiURL.String(), *cfg.App, c.http, func() time.Time { return c.now() })
	if err != nil {
		return err
	}
	c.authorize = tokens.authorize
	return nil
}

// getOne decodes the JSON at path into out.
func (c *Client) getOne(ctx context.Context, path string, out any) error {
	_, err := c.get(ctx, path, c.apiURL.String()+path, out)
	return err
}

// listAll reads every page of the list at path.
func listAll[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	return listTagged[T](ctx, c, path, query, nil)
}

// listTagged is listAll, keeping the list's ETag in t unless t is nil.
func listTagged[T any](ctx context.Context, c *Client, path string, query url.Values, t *tagger) ([]T, error) {
	var all []T
	err := paginate(ctx, c, path, query, t, func(page []T) { all = append(all, page...) })
	return all, err
}

// paginate reads the list at path page by page, following the Link
// header's next URL, and passes each page, decoded into a P, to each. A
// non-nil t keeps the list's ETag, as tagger.note says.
func paginate[P any](ctx context.Context, c *Client, path string, query url.Values, t *tagger,
	each func(P)) error {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("per_page", strconv.Itoa(c.perPage))
	first := c.apiURL.String() + path + "?" + q.Encode()
	target := first
	for n := 1; target != ""; n++ {
		if n > c.maxPages {
			return fmt.Errorf("github: GET %s: more than %d pages", path, c.maxPages)
		}
		var page P
		header, err := c.get(ctx, path, target, &page)
		if err != nil {
			return err
		}
		each(page)
		if target, err = c.nextPage(header, path); err != nil {
			return err
		}
		t.note(c.relative(first), n == 1 && target == "", header)
	}
	return nil
}

// nextPage returns the Link header's next URL, or "" on the last page. It
// refuses a URL on another host: the token must not be sent there.
func (c *Client) nextPage(h http.Header, path string) (string, error) {
	next := linkNext(h.Values(headerLink))
	if next == "" {
		return "", nil
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != c.apiURL.Scheme || u.Host != c.apiURL.Host ||
		!strings.HasPrefix(u.Path, c.apiURL.Path+"/") {
		return "", fmt.Errorf("github: GET %s: next page outside the API", path)
	}
	return next, nil
}

// linkNext returns the target of the rel="next" link in Link header values
// (`<url>; rel="next", <url>; rel="last"`), or "".
func linkNext(values []string) string {
	for _, value := range values {
		for link := range strings.SplitSeq(value, ",") {
			target, params, ok := strings.Cut(link, ";")
			if !ok {
				continue
			}
			for param := range strings.SplitSeq(params, ";") {
				if strings.TrimSpace(param) == `rel="next"` {
					return strings.Trim(strings.TrimSpace(target), "<>")
				}
			}
		}
	}
	return ""
}

// get decodes the JSON at target, a URL of path, into out and returns the
// response headers. It waits out rate limits within maxWait and
// maxRetries. Errors name the method and path, never the token or the full
// URL.
func (c *Client) get(ctx context.Context, path, target string, out any) (http.Header, error) {
	for attempt := 0; ; attempt++ {
		if err := c.awaitBudget(ctx, path); err != nil {
			return nil, wrapContext(path, err)
		}
		resp, err := c.send(ctx, path, target, "")
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK {
			return c.decode(resp, path, out)
		}
		wait, limited := c.refusal(resp, attempt)
		if !limited {
			return nil, &StatusError{Path: path, Code: resp.StatusCode, Status: resp.Status}
		}
		if attempt >= c.maxRetries || wait > c.maxWait {
			return nil, &RateLimitError{Path: path, Wait: wait}
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, wrapContext(path, err)
		}
	}
}

// send sends one GET of target, a URL of path, conditional on etag
// unless it is empty, and counts it and the budget it reports.
func (c *Client) send(ctx context.Context, path, target, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("github: GET %s: %w", path, err)
	}
	if err := c.authorize(ctx, req); err != nil {
		return nil, fmt.Errorf("github: GET %s: %w", path, err)
	}
	req.Header.Set("Accept", mediaType)
	req.Header.Set(headerAPIVersion, apiVersion)
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set(headerIfNoneMatch, etag)
	}
	c.requests.Add(1)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: GET %s: %w", path, unwrapURLError(err))
	}
	c.noteLimit(resp.Header)
	return resp, nil
}

// refusal reads and closes a refused response and says whether it is a
// rate limit and how long to wait before the attempt-th retry.
func (c *Client) refusal(resp *http.Response, attempt int) (wait time.Duration, limited bool) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, drainBytes))
	if !rateLimited(resp.StatusCode, resp.Header, body) {
		return 0, false
	}
	return retryWait(resp.Header, c.now(), attempt), true
}

func (c *Client) decode(resp *http.Response, path string, out any) (http.Header, error) {
	defer resp.Body.Close()
	// Reading to EOF after the value lets the connection be reused.
	defer func() { _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes)) }()
	c.noteBudget(resp.Header)
	body := http.MaxBytesReader(nil, resp.Body, c.maxBody)
	if err := json.NewDecoder(body).Decode(out); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, fmt.Errorf("github: GET %s: response larger than %d bytes", path, c.maxBody)
		}
		return nil, fmt.Errorf("github: GET %s: decoding: %w", path, err)
	}
	return resp.Header, nil
}

// wrapContext names the path on a context error, keeping it matchable
// with errors.Is; a *RateLimitError already names it.
func wrapContext(path string, err error) error {
	if _, ok := errors.AsType[*RateLimitError](err); ok {
		return err
	}
	return fmt.Errorf("github: GET %s: %w", path, err)
}

// unwrapURLError drops *url.Error's copy of the request URL, so an error
// never repeats the full target; get already names the path.
func unwrapURLError(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}
