package marker

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/store"
)

// outcome is what the endpoint did with a request, a metrics label.
type outcome string

// The outcomes besides the store's results.
const (
	outcomeInvalid      outcome = "invalid"
	outcomeUnauthorized outcome = "unauthorized"
	outcomeUnavailable  outcome = "unavailable"
)

// outcomes are every outcome, so that each series exists from the start.
var outcomes = []outcome{
	outcome(store.MarkerStored), outcome(store.MarkerDuplicate), outcome(store.MarkerConflict),
	outcome(store.MarkerLimited), outcomeInvalid, outcomeUnauthorized, outcomeUnavailable,
}

// Metrics count the phase markers sillage received.
type Metrics struct {
	markers *prometheus.CounterVec
}

// NewMetrics registers the marker metrics with reg.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	if reg == nil {
		return nil, errors.New("marker: the metrics need a registry")
	}
	m := &Metrics{markers: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sillage",
		Name:      "phase_markers_total",
		Help: "Phase marker requests sillage received, by outcome: stored, duplicate (an identical re-send), " +
			"conflict (another marker under its pod UID, span and edge), limited (past the limit per pod), " +
			"invalid (a body refused), unauthorized (a token missing or refused), unavailable (the token review " +
			"or the store failed). They count what sillage received, not what the pods sent.",
	}, []string{"outcome"})}
	if err := reg.Register(m.markers); err != nil {
		return nil, fmt.Errorf("marker: registering the metrics: %w", err)
	}
	for _, o := range outcomes {
		m.markers.WithLabelValues(string(o))
	}
	return m, nil
}

func (m *Metrics) count(o outcome) { m.markers.WithLabelValues(string(o)).Inc() }
