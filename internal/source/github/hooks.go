package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// hookSeparator separates a Hook's repository from its id: owner/name:id.
const hookSeparator = ":"

// Hook is one of sillage's own repository webhooks, whose delivery log
// tells which deliveries sillage did not receive.
type Hook struct {
	Repository Repository
	ID         int64
}

// String returns owner/name:id, as ParseHook reads it.
func (h Hook) String() string {
	return h.Repository.String() + hookSeparator + strconv.FormatInt(h.ID, 10)
}

// ParseHook parses "owner/name:id", id a positive hook id.
func ParseHook(s string) (Hook, error) {
	repo, id, ok := strings.Cut(strings.TrimSpace(s), hookSeparator)
	if !ok {
		return Hook{}, fmt.Errorf("github: hook %q: want owner/name:id", s)
	}
	r, err := ParseRepository(repo)
	if err != nil {
		return Hook{}, fmt.Errorf("github: hook %q: want owner/name:id", s)
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return Hook{}, fmt.Errorf("github: hook %q: want owner/name:id, id a positive number", s)
	}
	return Hook{Repository: r, ID: n}, nil
}

// hookPath returns the REST path of the hook.
func (h Hook) hookPath() string {
	return h.Repository.apiPath() + "/hooks/" + strconv.FormatInt(h.ID, 10)
}

// apiPath returns the REST path of the hook's delivery log.
func (h Hook) apiPath() string {
	return h.hookPath() + "/deliveries"
}

// hookConfig is a hook as GET /repos/{owner}/{repo}/hooks/{hook_id}
// returns it, with where it sends its deliveries.
type hookConfig struct {
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

// HookURL returns the URL hook sends its deliveries to, which tells
// whether it is sillage's before anything is redelivered to it.
func (c *Client) HookURL(ctx context.Context, hook Hook) (string, error) {
	var h hookConfig
	if err := c.getOne(ctx, hook.hookPath(), &h); err != nil {
		return "", err
	}
	return h.Config.URL, nil
}

// HookDelivery is one attempt in a hook's delivery log, from GET
// /repos/{owner}/{repo}/hooks/{hook_id}/deliveries. A redelivery is an
// attempt of its own, with a new ID and the GUID of the delivery it
// repeats: the X-GitHub-Delivery header the receiver saw.
type HookDelivery struct {
	ID          int64     `json:"id"`
	GUID        string    `json:"guid"`
	DeliveredAt time.Time `json:"delivered_at"`
	Redelivery  bool      `json:"redelivery"`
	// StatusCode is what the receiver answered; 0 when it did not answer
	// (a timeout, a connection refused).
	StatusCode int    `json:"status_code"`
	Status     string `json:"status"`
	Event      string `json:"event"`
}

// Received reports whether the receiver answered the attempt with a 2xx.
func (d HookDelivery) Received() bool {
	return d.StatusCode >= http.StatusOK && d.StatusCode < http.StatusMultipleChoices
}

// HookDeliveries returns the attempts in hook's delivery log newer than
// the attempt after (every one, for 0) and delivered at or after since
// (any time, for zero), newest first, as GitHub lists them: it stops
// reading at the first page that reaches one that is not, so that a hook
// with nothing new costs one request.
func (c *Client) HookDeliveries(ctx context.Context, hook Hook, after int64, since time.Time) (
	[]HookDelivery, error) {
	var out []HookDelivery
	err := paginateWhile(ctx, c, hook.apiPath(), nil, nil, func(page []HookDelivery) bool {
		for _, d := range page {
			if d.ID <= after || d.DeliveredAt.Before(since) {
				return false
			}
			d.DeliveredAt = d.DeliveredAt.UTC()
			out = append(out, d)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// hookDeliveryDetail is one attempt as GET /repos/{owner}/{repo}/hooks/
// {hook_id}/deliveries/{delivery_id} returns it, with its request.
type hookDeliveryDetail struct {
	Event   string `json:"event"`
	Request struct {
		Payload json.RawMessage `json:"payload"`
	} `json:"request"`
}

// HookPayload returns the event and the payload of hook's attempt id, as
// GitHub sent them.
func (c *Client) HookPayload(ctx context.Context, hook Hook, id int64) (event string, payload []byte, err error) {
	var d hookDeliveryDetail
	if err := c.getOne(ctx, hook.apiPath()+"/"+strconv.FormatInt(id, 10), &d); err != nil {
		return "", nil, err
	}
	return d.Event, d.Request.Payload, nil
}

// Redeliver asks GitHub to send hook's attempt id again; GitHub answers
// 202 Accepted and sends it shortly after, a new attempt of the same GUID.
// It is the one request the client sends that changes anything on
// GitHub, and only to send a delivery to sillage's own webhook (its
// caller checks the hook's HookURL first): the
// credential needs write access to the repository's webhooks, without
// which GitHub refuses it (see Refused). A rate limit is a
// *RateLimitError, never waited out; any other answer a *StatusError.
func (c *Client) Redeliver(ctx context.Context, hook Hook, id int64) error {
	path := hook.apiPath() + "/" + strconv.FormatInt(id, 10) + "/attempts"
	if err := c.awaitBudget(ctx, path); err != nil {
		if _, ok := errors.AsType[*RateLimitError](err); ok {
			return err
		}
		return fmt.Errorf("github: POST %s: %w", path, err)
	}
	resp, err := c.send(ctx, http.MethodPost, path, c.apiURL.String()+path, "")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusAccepted {
		c.noteBudget(resp.Header)
		drain(resp)
		return nil
	}
	if wait, limited := c.refusal(resp, 0); limited {
		return &RateLimitError{Path: path, Wait: wait}
	}
	return &StatusError{Method: http.MethodPost, Path: path, Code: resp.StatusCode, Status: resp.Status}
}
