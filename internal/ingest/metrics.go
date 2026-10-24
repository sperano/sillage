package ingest

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/model"
)

// The outcomes of requests refused before they were known to be
// deliveries. Nothing is recorded of them: they are labels of the
// metrics, never recorded outcomes.
const (
	// outcomeRefused: not authenticated (no secret, too large, unsigned,
	// badly signed), or authenticated but with no GitHub delivery id or
	// event, or one too long.
	outcomeRefused model.DeliveryOutcome = "refused"
	// outcomeBusy: refused unread, while every slot for reading a body was
	// held, or because its sender gave up waiting for one; nothing is known
	// of its authenticity.
	outcomeBusy model.DeliveryOutcome = "busy"
)

// Metrics count the deliveries sillage received.
type Metrics struct {
	deliveries *prometheus.CounterVec
}

// NewMetrics registers the ingest metrics with reg.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	if reg == nil {
		return nil, errors.New("ingest: the metrics need a registry")
	}
	m := &Metrics{deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sillage",
		Name:      "webhook_deliveries_total",
		Help: "Webhook delivery attempts sillage received, by source and outcome: accepted (their roots' reads " +
			"were asked for), ignored (no root, or a duplicate), rejected (unreadable payload), failed (not " +
			"every read was asked for, or the attempt was not recorded: some reads may have been); refused (not " +
			"authenticated, or a GitHub delivery id or event missing or too long) or busy (not read: every " +
			"body slot held, or the sender gave up waiting), neither recorded. They count what sillage " +
			"received, not what any other subscriber received or did.",
	}, []string{"source", "outcome"})}
	if err := reg.Register(m.deliveries); err != nil {
		return nil, fmt.Errorf("ingest: registering the metrics: %w", err)
	}
	// Every series exists from the start, so that a rate over it is
	// defined before the first delivery.
	for _, s := range []model.Source{model.SourceVikunja, model.SourceGitHub} {
		for _, o := range []model.DeliveryOutcome{model.DeliveryAccepted, model.DeliveryIgnored,
			model.DeliveryRejected, model.DeliveryFailed, outcomeRefused, outcomeBusy} {
			m.deliveries.WithLabelValues(string(s), string(o))
		}
	}
	return m, nil
}

func (m *Metrics) count(source model.Source, outcome model.DeliveryOutcome) {
	m.deliveries.WithLabelValues(string(source), string(outcome)).Inc()
}
