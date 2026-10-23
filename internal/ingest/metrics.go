package ingest

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/model"
)

// outcomeRefused counts the requests refused before they were known to be
// deliveries: no secret, too large, unsigned, badly signed, or with no
// delivery id. Nothing is recorded of them.
const outcomeRefused = "refused"

// Metrics count the deliveries sillage received.
type Metrics struct {
	deliveries *prometheus.CounterVec
}

// NewMetrics registers the ingest metrics with reg.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
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
		for _, o := range []string{string(model.DeliveryAccepted), string(model.DeliveryIgnored),
			string(model.DeliveryRejected), string(model.DeliveryFailed), outcomeRefused} {
			m.deliveries.WithLabelValues(string(s), o)
		}
	}
	return m, nil
}

func (m *Metrics) count(source model.Source, outcome string) {
	m.deliveries.WithLabelValues(string(source), outcome).Inc()
}
