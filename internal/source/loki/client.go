package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	queryPath = "/loki/api/v1/query_range"
	// maxPages bounds the requests of one Query, so that a window cannot
	// loop forever.
	maxPages = 10000
	// errorBodyBytes bounds how much of an error answer an error quotes.
	errorBodyBytes = 256
	// maxProblems bounds Result.Problems.
	maxProblems = 5
	orgIDHeader = "X-Scope-OrgID"
)

// Client reads usage events from Loki. It is safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client for cfg, which must be valid.
func New(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.timeout()}}, nil
}

// Result is what one Query read.
type Result struct {
	// Events are the usage events, in the order Loki returned them. An
	// event repeated (a page continues from its last entry's time, which
	// it repeats) is repeated here; the store keeps one.
	Events []model.UsageEvent
	// Entries counts the log entries read, Skipped those of no interest
	// (other events, successful Codex requests), Malformed those that
	// should have been events but could not be normalized.
	Entries, Skipped, Malformed int
	// Pages counts the requests made.
	Pages int
	// Stalled counts the pages that were full and whose entries all shared
	// one timestamp: the next page started 1 ns later, so entries of that
	// timestamp beyond the page limit were not read.
	Stalled int
	// Problems are the first reasons entries were malformed.
	Problems []string
}

// Query reads and normalizes the entries from (inclusive) to to
// (exclusive), following the pages Loki cuts it into.
func (c *Client) Query(ctx context.Context, from, to time.Time) (Result, error) {
	var res Result
	limit, start := c.cfg.pageLimit(), from
	for start.Before(to) {
		if res.Pages >= maxPages {
			return res, fmt.Errorf("loki: more than %d pages in one window", maxPages)
		}
		page, err := c.page(ctx, start, to, limit)
		if err != nil {
			return res, err
		}
		res.Pages++
		last := start
		for _, e := range page {
			res.add(c.cfg, e)
			if e.at.After(last) {
				last = e.at
			}
		}
		if len(page) < limit {
			break
		}
		if !last.After(start) {
			last = last.Add(time.Nanosecond)
			res.Stalled++
		}
		start = last
	}
	return res, nil
}

func (r *Result) add(cfg Config, e entry) {
	r.Entries++
	ev, err := normalize(cfg, e)
	switch {
	case err != nil:
		r.Malformed++
		if len(r.Problems) < maxProblems {
			r.Problems = append(r.Problems, err.Error())
		}
	case ev == nil:
		r.Skipped++
	default:
		r.Events = append(r.Events, *ev)
	}
}

// selector is the LogQL stream selector of the usage events.
func (c Config) selector() string {
	return `{k8s_namespace_name=` + strconv.Quote(c.Namespace) + `, service_name=~"` + serviceClaudeCode + `|` +
		serviceCodex + `"}`
}

func (c *Client) page(ctx context.Context, start, end time.Time, limit int) ([]entry, error) {
	q := url.Values{
		"query":     {c.cfg.selector()},
		"start":     {strconv.FormatInt(start.UnixNano(), 10)},
		"end":       {strconv.FormatInt(end.UnixNano(), 10)},
		"limit":     {strconv.Itoa(limit)},
		"direction": {"forward"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+queryPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("loki: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.cfg.OrgID != "" {
		req.Header.Set(orgIDHeader, c.cfg.OrgID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki: query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.maxResponseBytes()+1))
	if err != nil {
		return nil, fmt.Errorf("loki: reading the answer: %w", err)
	}
	if int64(len(body)) > c.cfg.maxResponseBytes() {
		return nil, fmt.Errorf("loki: the answer is over %d bytes", c.cfg.maxResponseBytes())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loki: query: %s: %s", resp.Status, quote(body))
	}
	var r queryResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("loki: decoding the answer: %w", err)
	}
	return r.entries()
}

func quote(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > errorBodyBytes {
		s = s[:errorBodyBytes] + "..."
	}
	return s
}
