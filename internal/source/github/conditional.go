package github

import (
	"context"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The headers of a conditional request and of the rate limit's size.
const (
	headerETag        = "ETag"
	headerIfNoneMatch = "If-None-Match"
	// headerRateLimit is the number of requests the window allows.
	headerRateLimit = "X-RateLimit-Limit"
)

// Validators are the ETags of what a branch read can tell unchanged from,
// by request target (path and query, without the API URL): its pulls
// list, and each pull request's timeline and head commit's check runs.
// GitHub changes a pull request in the list when it is updated (a comment,
// a review, a push, a state change), and its timeline and head checks
// cover the rest the backstop must not miss. The other lists of a read
// are not compared.
type Validators map[string]string

// tagger keeps the ETags of one branch read's lists. A nil tagger keeps
// nothing. A list that took more than one page, or came without an ETag,
// makes the read's validators incomplete: GitHub appends to a list's last
// page, which a first page's ETag does not see.
type tagger struct {
	tags    Validators
	missing bool
}

func newTagger() *tagger { return &tagger{tags: make(Validators)} }

// note keeps the ETag of the list at target, read in one page when
// onePage, from its response headers h.
func (t *tagger) note(target string, onePage bool, h http.Header) {
	if t == nil {
		return
	}
	etag := h.Get(headerETag)
	if !onePage || etag == "" {
		t.missing = true
		return
	}
	t.tags[target] = etag
}

// incomplete says a list the validators need has no ETag.
func (t *tagger) incomplete() {
	if t != nil {
		t.missing = true
	}
}

// validators returns what t kept; nil when a list had no ETag.
func (t *tagger) validators() Validators {
	if t.missing {
		return nil
	}
	return t.tags
}

// relative strips the API URL from target, a URL under it.
func (c *Client) relative(target string) string {
	return strings.TrimPrefix(target, c.apiURL.String())
}

// FetchBranchValidated is FetchBranch, and the validators that tell, by
// Unchanged, whether a later read would find the same: nil when one of
// the lists they need took more than one page, came without an ETag, or
// was refused.
func (c *Client) FetchBranchValidated(ctx context.Context, base Repository, head Branch) (
	[]Snapshot, Validators, error) {
	t := newTagger()
	snaps, err := c.fetchBranch(ctx, base, head, t)
	if err != nil {
		return nil, nil, err
	}
	return snaps, t.validators(), nil
}

// Unchanged reports whether every list v names is as it was when its ETag
// was kept: GitHub answers each request 304 Not Modified, which its rate
// limit does not count. It stops at the first list that changed, whose
// body it discards. A rate limit is a *RateLimitError, any other answer a
// *StatusError; it never waits a rate limit out.
func (c *Client) Unchanged(ctx context.Context, v Validators) (bool, error) {
	for _, target := range slices.Sorted(maps.Keys(v)) {
		path, _, _ := strings.Cut(target, "?")
		if err := c.awaitBudget(ctx, path); err != nil {
			return false, wrapContext(path, err)
		}
		resp, err := c.send(ctx, path, c.apiURL.String()+target, v[target])
		if err != nil {
			return false, err
		}
		switch resp.StatusCode {
		case http.StatusNotModified:
			c.notModified.Add(1)
			drain(resp)
			continue
		case http.StatusOK:
			c.noteBudget(resp.Header)
			drain(resp)
			return false, nil
		}
		if wait, limited := c.refusal(resp, 0); limited {
			return false, &RateLimitError{Path: path, Wait: wait}
		}
		return false, &StatusError{Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	return true, nil
}

// drain discards the rest of resp's body, up to drainBytes, and closes
// it, so that the connection can be reused.
func drain(resp *http.Response) {
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
}

// Budget is the rate limit's window as the last response reported it.
// Limit is 0 while no response has reported one.
type Budget struct {
	Limit, Remaining int
	// Reset is when the window resets.
	Reset time.Time
}

// Usage is what the client has asked of GitHub since it was made.
type Usage struct {
	// Requests counts the requests sent; NotModified those of them
	// answered 304 Not Modified, which the rate limit does not count.
	Requests, NotModified int64
	Budget                Budget
}

// Usage returns what the client has asked of GitHub so far.
func (c *Client) Usage() Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Usage{Requests: c.requests.Load(), NotModified: c.notModified.Load(), Budget: c.budget}
}

// noteLimit records the rate limit a response's headers report, if they
// report one whole.
func (c *Client) noteLimit(h http.Header) {
	limit, lerr := strconv.Atoi(h.Get(headerRateLimit))
	remaining, rerr := strconv.Atoi(h.Get(headerRateRemaining))
	reset, serr := strconv.ParseInt(h.Get(headerRateReset), 10, 64)
	if lerr != nil || rerr != nil || serr != nil || limit <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.budget = Budget{Limit: limit, Remaining: remaining, Reset: time.Unix(reset, 0).UTC()}
}
