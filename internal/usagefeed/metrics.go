package usagefeed

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "sillage"
	labelStage      = "stage"
	// The stages a cycle can fail at, as labelStage values.
	stageRead       = "read"
	stageSave       = "save"
	stageCheckpoint = "checkpoint"
)

// Metrics are what the feed exposes about itself.
type Metrics struct {
	read        prometheus.Counter
	inserted    prometheus.Counter
	duplicates  prometheus.Counter
	skipped     prometheus.Counter
	malformed   prometheus.Counter
	stalled     prometheus.Counter
	errors      *prometheus.CounterVec
	readThrough prometheus.Gauge
}

// NewMetrics registers the feed's metrics with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	counter := func(name, help string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{Namespace: metricNamespace, Name: name, Help: help})
	}
	m := &Metrics{
		read:       counter("usage_events_read_total", "Usage events read from the telemetry store, the overlap re-read each cycle included."),
		inserted:   counter("usage_events_inserted_total", "Usage events read that the store did not hold yet."),
		duplicates: counter("usage_events_duplicate_total", "Usage events read that the store held already (every overlap re-read is one)."),
		skipped:    counter("usage_entries_skipped_total", "Telemetry entries read that carry no usage."),
		malformed: counter("usage_entries_malformed_total",
			"Telemetry entries that should have been usage events and could not be normalized."),
		stalled: counter("usage_pages_stalled_total",
			"Full telemetry pages whose entries shared one timestamp, so that later entries of it may be unread."),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "usage_feed_errors_total",
			Help: "Usage feed windows that failed, by the stage that failed.",
		}, []string{labelStage}),
		readThrough: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace, Name: "usage_feed_read_through_timestamp_seconds",
			Help: "The time before which the usage feed has read every event, in Unix seconds.",
		}),
	}
	reg.MustRegister(m.read, m.inserted, m.duplicates, m.skipped, m.malformed, m.stalled, m.errors, m.readThrough)
	return m
}
