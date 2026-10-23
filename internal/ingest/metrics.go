package ingest

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/model"
)

// outcomeRefused counts the requests refused before they were known to be
// deliveries: no secret, too large, unsigned, badly signed, or with no
// delivery id. Nothing is recorded of them: it is a label of the metrics,
// never a recorded outcome.
const outcomeRefused model.DeliveryOutcome = "refused"

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
			"were asked for), ignored, rejected, failed, or refused (not authenticated; not recorded). They count " +
			"what sillage received, not what any other subscriber received or did.",
	}, []string{"source", "outcome"})}
	if err := reg.Register(m.deliveries); err != nil {
		return nil, fmt.Errorf("ingest: registering the metrics: %w", err)
	}
	// Every series exists from the start, so that a rate over it is
	// defined before the first delivery.
	for _, s := range []model.Source{model.SourceVikunja, model.SourceGitHub} {
		for _, o := range []model.DeliveryOutcome{model.DeliveryAccepted, model.DeliveryIgnored,
			model.DeliveryRejected, model.DeliveryFailed, outcomeRefused} {
			m.deliveries.WithLabelValues(string(s), string(o))
		}
	}
	return m, nil
}

func (m *Metrics) count(source model.Source, outcome model.DeliveryOutcome) {
	m.deliveries.WithLabelValues(string(source), string(outcome)).Inc()
}
