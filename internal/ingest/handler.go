package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

const (
	// Prefix is where the ingest paths are served.
	Prefix = "/ingest/"
	// PathVikunja receives the Vikunja webhooks' deliveries.
	PathVikunja = Prefix + "vikunja"
	// PathGitHub receives the GitHub repository webhooks' deliveries.
	PathGitHub = Prefix + "github"
	// MaxBodyBytes bounds a delivery's body: GitHub caps its payloads at
	// 25 MB (so does the proxy in front of sillage); a Vikunja task
	// payload stays far below.
	MaxBodyBytes = 25 << 20
	// bodyReadTimeout bounds reading a body, so that a client sending it
	// slowly cannot hold the request.
	bodyReadTimeout = time.Minute
	// storeTimeout bounds the store operations of one delivery.
	storeTimeout = 10 * time.Second
)

var errNotJSON = errors.New("the body is not a JSON payload (the webhook must send application/json)")

// storeError is a store operation that failed while settling a delivery.
type storeError struct {
	op  string
	err error
}

func (e *storeError) Error() string { return e.op + ": " + e.err.Error() }

func (e *storeError) Unwrap() error { return e.err }

// Store is what the handler needs from the store: *store.Store.
type Store interface {
	// DeliveryAccepted reports whether an attempt of the delivery was
	// accepted.
	DeliveryAccepted(ctx context.Context, source model.Source, instance, id string) (bool, error)
	// RecordDelivery records one attempt; store.ErrAlreadyAccepted for an
	// accepted one when an earlier attempt was accepted.
	RecordDelivery(ctx context.Context, d model.Delivery) error
	// RootsOfPullRequest returns the stored roots whose stories hold the
	// pull request.
	RootsOfPullRequest(ctx context.Context, key model.PRKey) ([]store.RootSummary, error)
}

var _ Store = (*store.Store)(nil)

// Scheduler asks the collector to read roots again.
type Scheduler interface {
	// Schedule asks for root to be read again soon, by the path the
	// collector's periodic reads take, and returns at once. It reports
	// false when it cannot take the request.
	Schedule(root model.Root) bool
}

// deliveries is one source's way of naming its deliveries and the roots
// they name.
type deliveries interface {
	source() model.Source
	// signature returns the request's signature header, and the prefix
	// before its hex digest.
	signature(h http.Header) (prefix, value string)
	// identify returns the delivery's id and event, or an error when the
	// delivery cannot be recorded without one.
	identify(h http.Header, body []byte) (id, event string, err error)
	// roots returns the roots the delivery names, or why it names none.
	// A storeError is the store failing; any other error a payload that
	// cannot be read.
	roots(ctx context.Context, event string, body []byte) ([]model.Root, string, error)
}

// Config is what the handler needs besides its store and scheduler.
type Config struct {
	// Collector names the source instances, and the roots of branches.
	Collector collector.Config
	Secrets   Secrets
}

// Handler serves the ingest paths.
type Handler struct {
	store   Store
	sched   Scheduler
	metrics *Metrics
	log     *slog.Logger
	now     func() time.Time
	// sources serve the paths, by path.
	sources map[string]source
}

// source is one ingest path's source.
type source struct {
	deliveries
	instance string
	secret   Secret
}

// New returns the handler of the ingest paths, recording deliveries in st
// and scheduling their roots' reads with sched.
func New(cfg Config, st Store, sched Scheduler, m *Metrics, log *slog.Logger) (*Handler, error) {
	if st == nil || sched == nil || m == nil || log == nil {
		return nil, errors.New("ingest: a store, a scheduler, metrics and a logger are required")
	}
	c := cfg.Collector
	if c.VikunjaInstance == "" || c.GitHubInstance == "" || c.WikiInstance == "" {
		return nil, errors.New("ingest: the Vikunja, GitHub and wiki instances are required")
	}
	return &Handler{
		store: st, sched: sched, metrics: m, log: log, now: time.Now,
		sources: map[string]source{
			PathVikunja: {deliveries: vikunjaDeliveries{instance: c.VikunjaInstance}, instance: c.VikunjaInstance,
				secret: cfg.Secrets.Vikunja},
			PathGitHub: {deliveries: githubDeliveries{cfg: c, store: st}, instance: c.GitHubInstance,
				secret: cfg.Secrets.GitHub},
		},
	}, nil
}

// ServeHTTP receives a delivery on PathVikunja or PathGitHub; any other
// path under Prefix is not found, and any other method not allowed.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	src, ok := h.sources[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	received := h.now()
	body, status, err := h.authenticate(w, r, src)
	if err != nil {
		h.refuse(w, src, status, err)
		return
	}
	id, event, err := src.identify(r.Header, body)
	if err != nil {
		h.refuse(w, src, http.StatusBadRequest, err)
		return
	}
	d := model.Delivery{Source: src.source(), Instance: src.instance, ID: id, Event: event, ReceivedAt: received}
	// An authenticated attempt is recorded even if its sender hangs up.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), storeTimeout)
	defer cancel()
	roots := h.settle(ctx, src, &d, body)
	recorded := h.record(ctx, &d)
	h.respond(w, d, roots, recorded)
}

// refuse answers a request that is no authenticated delivery: nothing is
// recorded of it.
func (h *Handler) refuse(w http.ResponseWriter, src source, status int, err error) {
	h.metrics.count(src.source(), outcomeRefused)
	h.log.Warn("webhook request refused: nothing recorded", "source", src.source(), "instance", src.instance,
		"status", status, "reason", err)
	http.Error(w, err.Error(), status)
}

// settle decides d's outcome and, when it is accepted, schedules the reads
// of the roots it names, which it returns.
func (h *Handler) settle(ctx context.Context, src source, d *model.Delivery, body []byte) []model.Root {
	accepted, err := h.store.DeliveryAccepted(ctx, d.Source, d.Instance, d.ID)
	if err != nil {
		h.fail(d, &storeError{op: "reading the earlier attempts", err: err})
		return nil
	}
	if accepted {
		d.Outcome, d.Detail = model.DeliveryIgnored, duplicateDetail
		return nil
	}
	roots, why, err := src.roots(ctx, d.Event, body)
	if se := (*storeError)(nil); errors.As(err, &se) {
		h.fail(d, se)
		return nil
	}
	if err != nil {
		d.Outcome, d.Detail = model.DeliveryRejected, err.Error()
		return nil
	}
	if len(roots) == 0 {
		d.Outcome, d.Detail = model.DeliveryIgnored, why
		return nil
	}
	for _, root := range roots {
		if !h.sched.Schedule(root) {
			d.Outcome, d.Detail = model.DeliveryFailed, queueFullDetail
			return roots
		}
	}
	d.Outcome = model.DeliveryAccepted
	return roots
}

// fail marks d failed on a store error, which is logged, not recorded.
func (h *Handler) fail(d *model.Delivery, se *storeError) {
	h.log.Error("webhook delivery: store failed", "source", d.Source, "delivery", d.ID, "error", se)
	d.Outcome, d.Detail = model.DeliveryFailed, "the store failed while "+se.op
}

// record records d, as a duplicate if an earlier attempt was accepted
// meanwhile, and reports whether it was recorded.
func (h *Handler) record(ctx context.Context, d *model.Delivery) bool {
	err := h.store.RecordDelivery(ctx, *d)
	if errors.Is(err, store.ErrAlreadyAccepted) {
		d.Outcome, d.Detail = model.DeliveryIgnored, duplicateDetail
		err = h.store.RecordDelivery(ctx, *d)
	}
	if err != nil {
		h.log.Error("webhook delivery: recording it failed", "source", d.Source, "delivery", d.ID, "error", err)
		return false
	}
	return true
}

// respond answers the delivery with its outcome, counts and logs it. A
// delivery that failed or could not be recorded is answered 503, which
// Vikunja retries.
func (h *Handler) respond(w http.ResponseWriter, d model.Delivery, roots []model.Root, recorded bool) {
	status, outcome := statusOf(d.Outcome), d.Outcome
	if !recorded {
		status, outcome = http.StatusServiceUnavailable, model.DeliveryFailed
	}
	h.metrics.count(d.Source, string(outcome))
	level := slog.LevelInfo
	if status >= http.StatusBadRequest {
		level = slog.LevelWarn
	}
	h.log.Log(context.Background(), level, "webhook delivery received", "source", d.Source, "instance", d.Instance,
		"delivery", d.ID, "event", d.Event, "outcome", outcome, "detail", d.Detail, "roots", len(roots),
		"recorded", recorded)
	text := string(outcome)
	if d.Detail != "" {
		text += ": " + d.Detail
	}
	if !recorded {
		text += " (the attempt could not be recorded)"
	}
	http.Error(w, text, status)
}

// statusOf is the HTTP status that answers an outcome: 2xx for what needs
// no retry, 400 for a payload a retry would not change, 503 otherwise.
func statusOf(o model.DeliveryOutcome) int {
	switch o {
	case model.DeliveryAccepted:
		return http.StatusAccepted
	case model.DeliveryIgnored:
		return http.StatusOK
	case model.DeliveryRejected:
		return http.StatusBadRequest
	}
	return http.StatusServiceUnavailable
}

const (
	duplicateDetail = "duplicate: an earlier attempt of this delivery was accepted"
	queueFullDetail = "the collector's queue of reads is full: its next periodic read catches up"
)

// authenticate reads the body, at most MaxBodyBytes within
// bodyReadTimeout, and checks its signature. It returns the status that
// refuses the request otherwise.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request, src source) ([]byte, int, error) {
	if src.secret == "" {
		return nil, http.StatusForbidden, fmt.Errorf("no %s webhook secret is configured: every delivery is refused",
			src.source())
	}
	body, status, err := readBody(w, r)
	if err != nil {
		return nil, status, err
	}
	prefix, signature := src.signature(r.Header)
	if err := verify(src.secret, prefix, signature, body); err != nil {
		return nil, http.StatusUnauthorized, err
	}
	return body, 0, nil
}
