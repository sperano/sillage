package marker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sperano/sillage/internal/ingest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/store"
)

const (
	// Path receives the phase markers.
	Path = ingest.Prefix + "marker"
	// PerPodLimit bounds the markers kept of one pod, so that a runaway
	// loop of wrapper calls cannot fill the store.
	PerPodLimit = 2000
	// storeTimeout bounds recording a marker.
	storeTimeout = 5 * time.Second
	// bodyReadTimeout bounds reading a body.
	bodyReadTimeout = 5 * time.Second
	// maxBodiesInFlight bounds the bodies read at once, so that a pod
	// holding many slow requests holds at most this many bodies in memory.
	maxBodiesInFlight = 16
	// bodySlotWait is how long a request waits for a body slot.
	bodySlotWait = time.Second
)

var errBodiesBusy = errors.New("too many markers are being read: try again")

// Store is what the handler needs from the store: *store.Store.
type Store interface {
	// RecordMarker stores m unless one of its pod UID, span and edge is
	// held, or perPod of its pod.
	RecordMarker(ctx context.Context, m model.Marker, perPod int) (store.MarkerResult, error)
}

var _ Store = (*store.Store)(nil)

// Config is what the handler needs to name a marker's sender.
type Config struct {
	// Cluster names the cluster on every sender, as on the Kelos source's
	// run keys.
	Cluster string
	// Namespace is the Kelos namespace: only its ServiceAccounts are
	// accepted.
	Namespace string
}

// Handler serves Path.
type Handler struct {
	cfg     Config
	auth    *authenticator
	store   Store
	metrics *Metrics
	log     *slog.Logger
	now     func() time.Time
	// bodies holds a slot for each body being read: at most
	// maxBodiesInFlight, however many requests arrive.
	bodies chan struct{}
	// bodyWait is how long a request waits for a body slot: bodySlotWait.
	bodyWait time.Duration
}

// New returns the handler of Path, reviewing tokens with rev and storing
// markers in st.
func New(cfg Config, rev Reviewer, st Store, m *Metrics, log *slog.Logger) (*Handler, error) {
	if rev == nil || st == nil || m == nil || log == nil {
		return nil, errors.New("marker: a reviewer, a store, metrics and a logger are required")
	}
	if cfg.Cluster == "" || cfg.Namespace == "" {
		return nil, errors.New("marker: the Kelos cluster and namespace are required")
	}
	h := &Handler{cfg: cfg, store: st, metrics: m, log: log, now: time.Now,
		bodies: make(chan struct{}, maxBodiesInFlight), bodyWait: bodySlotWait}
	h.auth = newAuthenticator(rev, cfg.Namespace, func() time.Time { return h.now() })
	return h, nil
}

// ServeHTTP receives one marker on Path.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != Path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	// A body declared too large costs no review.
	if r.ContentLength > MaxBodyBytes {
		h.refuse(r.Context(), w, tooLarge())
		return
	}
	// A marker's times are UTC, whatever the host's zone.
	received := h.now().UTC()
	id, err := h.auth.authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		h.refuse(r.Context(), w, err)
		return
	}
	report, err := h.readReport(w, r, received)
	if err != nil {
		h.refuse(r.Context(), w, err)
		return
	}
	m := model.Marker{
		Report: report,
		Sender: model.MarkerSender{Cluster: h.cfg.Cluster, Namespace: id.namespace,
			ServiceAccount: id.serviceAccount, Pod: id.pod, PodUID: id.podUID},
		ReceivedAt: received,
	}
	h.record(context.WithoutCancel(r.Context()), w, m)
}

// readReport reads and decodes the body, at most MaxBodyBytes, once a body
// slot is free.
func (h *Handler) readReport(w http.ResponseWriter, r *http.Request, received time.Time) (model.MarkerReport, error) {
	wait := time.NewTimer(h.bodyWait)
	defer wait.Stop()
	select {
	case h.bodies <- struct{}{}:
		defer func() { <-h.bodies }()
	case <-wait.C:
		return model.MarkerReport{}, &refusal{status: http.StatusServiceUnavailable, err: errBodiesBusy}
	case <-r.Context().Done():
		return model.MarkerReport{}, &refusal{status: http.StatusServiceUnavailable, err: errBodiesBusy}
	}
	// A writer that cannot set deadlines (a test recorder) has its
	// server's timeouts only.
	_ = http.NewResponseController(w).SetReadDeadline(h.now().Add(bodyReadTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe):
		return model.MarkerReport{}, tooLarge()
	case err != nil:
		return model.MarkerReport{}, &refusal{status: http.StatusBadRequest, err: errors.New("the body could not be read")}
	}
	report, err := decodeReport(body, received)
	if err != nil {
		return model.MarkerReport{}, &refusal{status: http.StatusBadRequest, err: err}
	}
	return report, nil
}

func tooLarge() *refusal {
	return &refusal{status: http.StatusRequestEntityTooLarge,
		err: fmt.Errorf("the body is larger than %d bytes", MaxBodyBytes)}
}

// refuse answers a request refused before it was recorded. Anything in the
// cluster can send one, so it is logged at debug level, unless sillage
// itself failed.
func (h *Handler) refuse(ctx context.Context, w http.ResponseWriter, err error) {
	var ae *refusal
	if !errors.As(err, &ae) {
		ae = &refusal{status: http.StatusBadRequest, err: err}
	}
	o, level := outcomeInvalid, slog.LevelDebug
	switch ae.status {
	case http.StatusUnauthorized:
		o = outcomeUnauthorized
		w.Header().Set("WWW-Authenticate", bearerScheme)
	case http.StatusServiceUnavailable:
		o, level = outcomeUnavailable, slog.LevelWarn
	}
	h.metrics.count(o)
	h.log.Log(ctx, level, "phase marker refused", "status", ae.status, "reason", ae.err)
	http.Error(w, ae.answer(), ae.status)
}

// record stores m and answers with what the store did.
func (h *Handler) record(ctx context.Context, w http.ResponseWriter, m model.Marker) {
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	result, err := h.store.RecordMarker(ctx, m, PerPodLimit)
	if err != nil {
		h.metrics.count(outcomeUnavailable)
		h.log.ErrorContext(ctx, "phase marker: the store failed", "pod", m.Sender.Pod, "error", err)
		http.Error(w, "the marker could not be recorded", http.StatusServiceUnavailable)
		return
	}
	h.metrics.count(outcome(result))
	status, level := statusOf(result)
	h.log.Log(ctx, level, "phase marker received", "result", result, "kind", m.Report.Kind,
		"edge", m.Report.Edge, "span", m.Report.Span, "pod", m.Sender.Pod, "podUID", m.Sender.PodUID)
	http.Error(w, string(result), status)
}

// statusOf answers a store result: 2xx for what needs nothing more, a
// client error otherwise, and how loudly to log it.
func statusOf(r store.MarkerResult) (int, slog.Level) {
	switch r {
	case store.MarkerStored:
		return http.StatusAccepted, slog.LevelDebug
	case store.MarkerDuplicate:
		return http.StatusOK, slog.LevelDebug
	case store.MarkerConflict:
		return http.StatusConflict, slog.LevelWarn
	}
	return http.StatusTooManyRequests, slog.LevelWarn
}
