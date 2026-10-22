package wiki

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// graphQLPath is where Wiki.js 2 serves GraphQL under the base URL.
	graphQLPath = "/graphql"
	// DefaultRequestTimeout bounds each HTTP request of the default client.
	DefaultRequestTimeout = 30 * time.Second
	// defaultMaxResponseBytes bounds the body read from any one response.
	defaultMaxResponseBytes = 16 << 20
	// drainBytes is how much of an error response is read and discarded so
	// the connection can be reused.
	drainBytes = 4 << 10
	// messageForbidden is the error Wiki.js's @auth directive raises for a
	// field the token lacks the permission for.
	messageForbidden = "Forbidden"
	// codePageViewForbidden is Wiki.js's PageViewForbidden error code:
	// pages.single needs manage:pages or delete:pages on the page.
	codePageViewForbidden = 6013
)

// StatusError is an HTTP response other than 200 OK.
type StatusError struct {
	// Operation is the GraphQL operation sent.
	Operation string
	// Code is the HTTP status code; Status its full text ("401 Unauthorized").
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return "wiki: " + e.Operation + ": " + e.Status
}

// ResponseError is one entry of a GraphQL reply's errors.
type ResponseError struct {
	Message    string `json:"message"`
	Extensions struct {
		Exception struct {
			// Code is Wiki.js's own error code; 0 for errors it raises
			// without one (the @auth directive's).
			Code int `json:"code"`
		} `json:"exception"`
	} `json:"extensions"`
}

// GraphQLError is a reply that carries errors.
type GraphQLError struct {
	Operation string
	Errors    []ResponseError
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, re := range e.Errors {
		msgs[i] = re.Message
	}
	return "wiki: " + e.Operation + ": " + strings.Join(msgs, "; ")
}

// forbidden reports whether every error refuses access rather than
// reporting a missing record or a failure.
func (e *GraphQLError) forbidden() bool {
	for _, re := range e.Errors {
		if re.Message != messageForbidden && re.Extensions.Exception.Code != codePageViewForbidden {
			return false
		}
	}
	return len(e.Errors) > 0
}

// Client reads pages, their history and their comments from one Wiki.js 2
// instance. It sends only the queries in fetch.go, never a mutation. It
// holds the token only inside authorize, so printing a Client never prints
// the token.
type Client struct {
	endpoint  string
	instance  string
	authorize func(*http.Request)
	http      *http.Client
	now       func() time.Time
	// The fields below are fields so tests can shrink them.
	historyPageSize int
	maxHistoryPages int
	maxBody         int64
}

// NewClient returns a client for cfg, which must be valid (ConfigFromEnv
// returns only valid ones). A nil hc gets a client with
// DefaultRequestTimeout.
func NewClient(cfg Config, hc *http.Client) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Instance == "" {
		return nil, errors.New("wiki: an instance name is required")
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	header := "Bearer " + string(cfg.Token)
	return &Client{
		endpoint:        strings.TrimRight(cfg.BaseURL, "/") + graphQLPath,
		instance:        cfg.Instance,
		authorize:       func(r *http.Request) { r.Header.Set("Authorization", header) },
		http:            hc,
		now:             time.Now,
		historyPageSize: defaultHistoryPageSize,
		maxHistoryPages: defaultMaxHistoryPages,
		maxBody:         defaultMaxResponseBytes,
	}, nil
}

// graphQLRequest is a GraphQL request body.
type graphQLRequest struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
}

// graphQLReply is a GraphQL reply body.
type graphQLReply struct {
	Data   json.RawMessage `json:"data"`
	Errors []ResponseError `json:"errors"`
}

// query sends operation op, whose GraphQL document is doc, and decodes its
// data into out. A reply with errors is a *GraphQLError, whatever data it
// also carries. Errors name the operation, never the token or the URL.
func (c *Client) query(ctx context.Context, op, doc string, vars map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{OperationName: op, Query: doc, Variables: vars})
	if err != nil {
		return fmt.Errorf("wiki: %s: %w", op, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("wiki: %s: %w", op, unwrapURLError(err))
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("wiki: %s: %w", op, unwrapURLError(err))
	}
	defer resp.Body.Close()
	// Reading to EOF after the reply lets the connection be reused.
	defer func() { _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes)) }()
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Operation: op, Code: resp.StatusCode, Status: resp.Status}
	}
	return c.decode(op, resp.Body, out)
}

// decode reads a reply from body, a ReadCloser as http.MaxBytesReader
// needs.
func (c *Client) decode(op string, body io.ReadCloser, out any) error {
	var r graphQLReply
	if err := json.NewDecoder(http.MaxBytesReader(nil, body, c.maxBody)).Decode(&r); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return fmt.Errorf("wiki: %s: response larger than %d bytes", op, c.maxBody)
		}
		return fmt.Errorf("wiki: %s: decoding: %w", op, err)
	}
	if len(r.Errors) > 0 {
		return &GraphQLError{Operation: op, Errors: r.Errors}
	}
	if err := json.Unmarshal(r.Data, out); err != nil {
		return fmt.Errorf("wiki: %s: decoding data: %w", op, err)
	}
	return nil
}

// unwrapURLError drops *url.Error's copy of the request URL, so an error
// never repeats the full target; query already names the operation.
func unwrapURLError(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}
