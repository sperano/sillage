package github

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"time"
)

// GitHub's rate-limit headers and the back-off they drive. See
// https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api
const (
	// headerRateRemaining is the number of requests left in the window.
	headerRateRemaining = "X-RateLimit-Remaining"
	// headerRateReset is when the window resets, in Unix seconds.
	headerRateReset = "X-RateLimit-Reset"
	// headerRetryAfter is how many seconds to wait after a secondary
	// rate limit.
	headerRetryAfter = "Retry-After"
	// rateLimitExhausted is headerRateRemaining's value once no request is
	// left.
	rateLimitExhausted = "0"
	// rateLimitBodyMarker appears in the message of a 403 that is a
	// secondary rate limit sent without a header saying so.
	rateLimitBodyMarker = "rate limit"
	// secondaryLimitWait is the first wait after a rate limit that names no
	// time; GitHub asks for at least one minute. Each further retry doubles
	// it.
	secondaryLimitWait = time.Minute
	// resetMargin is added to a reset time, against clock skew between
	// GitHub and this host.
	resetMargin = time.Second
	// DefaultMaxRateLimitWait is the longest the client waits for a rate
	// limit to lift; a longer wait is returned as a *RateLimitError, so
	// the caller decides rather than blocking for up to an hour.
	DefaultMaxRateLimitWait = 15 * time.Minute
	// defaultMaxRateLimitRetries is how many times one request is retried
	// after rate-limited responses before giving up.
	defaultMaxRateLimitRetries = 3
)

// RateLimitError is a request GitHub refused for a rate limit and the
// client did not retry: the wait was longer than its limit, or the retries
// ran out.
type RateLimitError struct {
	// Path is the API path requested, without the base URL and query.
	Path string
	// Wait is how long GitHub asked to wait.
	Wait time.Duration
}

func (e *RateLimitError) Error() string {
	return "github: GET " + e.Path + ": rate limited, retry after " + e.Wait.String()
}

// rateLimited reports whether a response is a primary or secondary rate
// limit rather than an ordinary error. body is the start of the response
// body.
func rateLimited(code int, h http.Header, body []byte) bool {
	switch code {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return h.Get(headerRateRemaining) == rateLimitExhausted ||
			h.Get(headerRetryAfter) != "" ||
			bytes.Contains(bytes.ToLower(body), []byte(rateLimitBodyMarker))
	}
	return false
}

// retryWait is how long to wait before retrying a rate-limited request,
// the attempt-th retry (from 0): Retry-After when set, else until the
// window resets when it is exhausted, else secondaryLimitWait doubled per
// attempt.
func retryWait(h http.Header, now time.Time, attempt int) time.Duration {
	if s, err := strconv.Atoi(h.Get(headerRetryAfter)); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if until, ok := exhaustedUntil(h); ok {
		return max(until.Sub(now), 0)
	}
	return secondaryLimitWait << attempt
}

// exhaustedUntil returns when requests may resume, and true, when the
// headers say no request is left in the window.
func exhaustedUntil(h http.Header) (time.Time, bool) {
	if h.Get(headerRateRemaining) != rateLimitExhausted {
		return time.Time{}, false
	}
	reset, err := strconv.ParseInt(h.Get(headerRateReset), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(reset, 0).Add(resetMargin), true
}

// noteBudget records, from a response's headers, when the next request may
// be sent: after the reset when this one used the window's last request.
func (c *Client) noteBudget(h http.Header) {
	until, ok := exhaustedUntil(h)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if until.After(c.resumeAt) {
		c.resumeAt = until
	}
}

// awaitBudget waits until the rate-limit window noted by noteBudget has
// reset, or returns a *RateLimitError when that is more than maxWait away.
func (c *Client) awaitBudget(ctx context.Context, path string) error {
	c.mu.Lock()
	wait := c.resumeAt.Sub(c.now())
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	if wait > c.maxWait {
		return &RateLimitError{Path: path, Wait: wait}
	}
	return c.sleep(ctx, wait)
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
