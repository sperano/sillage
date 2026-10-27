package ingest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

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
	// storeTimeout bounds deciding a delivery's outcome, which records an
	// accepted one, and then again recording any other.
	storeTimeout = 10 * time.Second
)

const (
	duplicateDetail = "duplicate: an earlier attempt of this delivery was accepted"
	queueFullDetail = "the collector's queue of reads is full: its next periodic read catches up"
)

// errQueueFull is the scheduler refusing a read.
var errQueueFull = errors.New(queueFullDetail)

// Store is what the handler needs from the store: *store.Store.
type Store interface {
	// DeliveryAccepted reports whether an attempt of the delivery was
	// accepted.
	DeliveryAccepted(ctx context.Context, source model.Source, instance, id string) (bool, error)
	// AcceptDelivery records an accepted attempt and runs act in one
	// transaction, so that of two attempts racing only one runs it;
	// store.ErrAlreadyAccepted, act not run, when an attempt was accepted.
	AcceptDelivery(ctx context.Context, d model.Delivery, act func() error) error
	// RecordDelivery records one attempt that was not accepted.
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

// parser is one source's way of naming its deliveries and the roots they
// name.
type parser interface {
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

// endpoint is one ingest path: its source instance, the secret its
// deliveries are signed with, and how to read them.
type endpoint struct {
	parser
	source   model.Source
	instance string
	secret   Secret
	// maxBody bounds a delivery's body.
	maxBody int64
}

// Handler serves the ingest paths.
type Handler struct {
	store   Store
	sched   Scheduler
	metrics *Metrics
	log     *slog.Logger
	now     func() time.Time
	// endpoints serve the paths, by path.
	endpoints map[string]endpoint
	// bodies holds a slot for each body being read: at most
	// maxBodiesInFlight, however many requests arrive.
	bodies chan struct{}
}

// New returns the handler of the ingest paths, recording deliveries in st
// and scheduling their roots' reads with sched.
func New(cfg Config, st Store, sched Scheduler, m *Metrics, log *slog.Logger) (*Handler, error) {
	if st == nil || sched == nil || m == nil || log == nil {
		return nil, errors.New("ingest: a store, a scheduler, metrics and a logger are required")
	}
	c := cfg.Collector
	if c.VikunjaInstance == "" || c.GitHubInstance == "" {
		return nil, errors.New("ingest: the Vikunja and GitHub instances are required")
	}
	return &Handler{
		store: st, sched: sched, metrics: m, log: log, now: time.Now,
		endpoints: map[string]endpoint{
			PathVikunja: {parser: vikunjaParser{instance: c.VikunjaInstance}, source: model.SourceVikunja,
				instance: c.VikunjaInstance, secret: cfg.Secrets.Vikunja, maxBody: MaxVikunjaBodyBytes},
			PathGitHub: {parser: githubParser{cfg: c, store: st}, source: model.SourceGitHub,
				instance: c.GitHubInstance, secret: cfg.Secrets.GitHub, maxBody: MaxGitHubBodyBytes},
		},
		bodies: make(chan struct{}, maxBodiesInFlight),
	}, nil
}

// ServeHTTP receives a delivery on PathVikunja or PathGitHub; any other
// path under Prefix is not found, and any other method not allowed.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ep, ok := h.endpoints[r.URL.Path]
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
	body, err := h.authenticate(w, r, ep)
	if err != nil {
		h.refuse(r.Context(), w, ep, err)
		return
	}
	id, event, err := ep.identify(r.Header, body)
	if err != nil {
		h.refuse(r.Context(), w, ep, &refusal{status: http.StatusBadRequest, err: err})
		return
	}
	d := model.Delivery{Source: ep.source, Instance: ep.instance, ID: id, Event: event, ReceivedAt: received}
	// An authenticated attempt is recorded even if its sender hangs up.
	ctx := context.WithoutCancel(r.Context())
	roots := h.settle(ctx, ep, &d, body)
	// settle recorded an accepted attempt along with scheduling its reads.
	recorded := d.Outcome == model.DeliveryAccepted || h.record(ctx, &d)
	h.respond(ctx, w, d, len(roots), recorded)
}

// refuse answers a request that is no authenticated delivery: nothing is
// recorded of it. Anyone can send one, so it is logged at debug level
// only; the metrics count it, as refused or busy.
func (h *Handler) refuse(ctx context.Context, w http.ResponseWriter, ep endpoint, err error) {
	rf := asRefusal(err)
	outcome := outcomeRefused
	if rf.busy {
		outcome = outcomeBusy
	}
	h.metrics.count(ep.source, outcome)
	h.log.DebugContext(ctx, "webhook request refused: nothing recorded", "source", ep.source,
		"instance", ep.instance, "status", rf.status, "reason", rf.err)
	http.Error(w, rf.err.Error(), rf.status)
}

// settle decides d's outcome and, when it is accepted, records it and
// schedules the reads of the roots it names, which it returns.
func (h *Handler) settle(ctx context.Context, ep endpoint, d *model.Delivery, body []byte) []model.Root {
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	accepted, err := h.store.DeliveryAccepted(ctx, d.Source, d.Instance, d.ID)
	if err != nil {
		h.fail(ctx, d, &storeError{op: "reading the earlier attempts", err: err})
		return nil
	}
	if accepted {
		d.Outcome, d.Detail = model.DeliveryIgnored, duplicateDetail
		return nil
	}
	roots, why, err := ep.roots(ctx, d.Event, body)
	var se *storeError
	switch {
	case errors.As(err, &se):
		h.fail(ctx, d, se)
		return nil
	case err != nil:
		d.Outcome, d.Detail = model.DeliveryRejected, err.Error()
		return nil
	case len(roots) == 0:
		d.Outcome, d.Detail = model.DeliveryIgnored, why
		return nil
	}
	return h.accept(ctx, d, roots)
}

// accept records d accepted and schedules the reads of roots in one store
// transaction, so that of two attempts of d racing, only one schedules
// them; the other is a duplicate. It returns roots unless d was a
// duplicate. A failed d can have scheduled some of the reads.
func (h *Handler) accept(ctx context.Context, d *model.Delivery, roots []model.Root) []model.Root {
	d.Outcome = model.DeliveryAccepted
	err := h.store.AcceptDelivery(ctx, *d, func() error {
		for _, root := range roots {
			if !h.sched.Schedule(root) {
				return errQueueFull
			}
		}
		return nil
	})
	switch {
	case err == nil:
	case errors.Is(err, store.ErrAlreadyAccepted):
		d.Outcome, d.Detail = model.DeliveryIgnored, duplicateDetail
		return nil
	case errors.Is(err, errQueueFull):
		d.Outcome, d.Detail = model.DeliveryFailed, queueFullDetail
	default:
		h.fail(ctx, d, &storeError{op: "recording its acceptance", err: err})
	}
	return roots
}

// fail marks d failed on a store error, which is logged, not recorded.
func (h *Handler) fail(ctx context.Context, d *model.Delivery, se *storeError) {
	h.log.ErrorContext(ctx, "webhook delivery: store failed", "source", d.Source, "delivery", d.ID, "error", se)
	d.Outcome, d.Detail = model.DeliveryFailed, "the store failed while "+se.op
}

// record records d, which was not accepted, and reports whether it was
// recorded.
func (h *Handler) record(ctx context.Context, d *model.Delivery) bool {
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	if err := h.store.RecordDelivery(ctx, *d); err != nil {
		h.log.ErrorContext(ctx, "webhook delivery: recording it failed", "source", d.Source, "delivery", d.ID,
			"error", err)
		return false
	}
	return true
}

// respond answers the delivery with its outcome, counts and logs it. A
// delivery that failed or could not be recorded is answered 503, which
// Vikunja retries.
func (h *Handler) respond(ctx context.Context, w http.ResponseWriter, d model.Delivery, roots int, recorded bool) {
	status, outcome := statusOf(d.Outcome), d.Outcome
	if !recorded {
		status, outcome = http.StatusServiceUnavailable, model.DeliveryFailed
	}
	h.metrics.count(d.Source, outcome)
	level := slog.LevelInfo
	if status >= http.StatusBadRequest {
		level = slog.LevelWarn
	}
	h.log.Log(ctx, level, "webhook delivery received", "source", d.Source, "instance", d.Instance,
		"delivery", d.ID, "event", d.Event, "outcome", outcome, "detail", d.Detail, "roots", roots,
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
