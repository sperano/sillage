package reconcile

import (
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

// The metrics' names and labels.
const (
	metricNamespace = "sillage"
	labelSource     = "source"
	labelInstance   = "instance"
	labelReason     = "reason"
	labelKind       = "kind"
	labelOperation  = "operation"
	labelResource   = "resource"
	labelCode       = "code"
	// The kinds of record written, as labelKind values.
	kindEvents       = "events"
	kindRuns         = "runs"
	kindPullRequests = "pull_requests"
	kindCorrelations = "correlations"
	// The store operations that can fail, as labelOperation values.
	operationLoad   = "load"
	operationRead   = "read"
	operationCommit = "commit"
)

// readDurationBuckets spread from a cached read to a GitHub branch that
// waits out a rate limit: 50 ms, doubling, to about 30 minutes.
var readDurationBuckets = prometheus.ExponentialBuckets(0.05, 2, 16)

// Metrics are what the loop exposes about itself.
type Metrics struct {
	lastSuccess  *prometheus.GaugeVec
	readErrors   *prometheus.CounterVec
	refusals     *prometheus.CounterVec
	readDuration *prometheus.HistogramVec
	written      *prometheus.CounterVec
	storeErrors  *prometheus.CounterVec
	cycles       prometheus.Counter
	roots        prometheus.Gauge
	lockHeld     prometheus.Gauge
	// deliveryReads and deliveryCycles are what webhook deliveries asked
	// of the loop.
	deliveryReads  *prometheus.CounterVec
	deliveryCycles prometheus.Counter
	// githubReads are the roots' GitHub sides by what read them, or did
	// not; githubRequests, githubNotModified and githubRemaining what the
	// GitHub client asked and was left.
	githubReads       *prometheus.CounterVec
	githubRequests    prometheus.Counter
	githubNotModified prometheus.Counter
	githubRemaining   prometheus.Gauge
}

// NewMetrics registers the loop's metrics with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	source := []string{labelSource, labelInstance}
	m := &Metrics{
		lastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricNamespace, Name: "source_last_success_timestamp_seconds",
			Help: "When the last successful read of a source instance started, in Unix seconds.",
		}, source),
		readErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "source_read_errors_total",
			Help: "Failed reads of a source instance, by reason: timeout, rate_limited, server_error or failed.",
		}, append(source, labelReason)),
		refusals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "source_refusals_total",
			Help: "Requests a source refused (403 or 404) within a read that succeeded, by resource and status code.",
		}, append(source, labelResource, labelCode)),
		readDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricNamespace, Name: "source_read_duration_seconds",
			Help: "How long one read of a source instance took, failed or not.", Buckets: readDurationBuckets,
		}, source),
		written: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "records_written_total",
			Help: "Records committed to the store, by kind; an unchanged story commits none.",
		}, []string{labelKind}),
		storeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "store_errors_total",
			Help: "Failed store operations, by operation: load, read or commit.",
		}, []string{labelOperation}),
		cycles: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "collector_cycles_total", Help: "Collection cycles completed.",
		}),
		roots: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace, Name: "collector_roots", Help: "Roots the collector has read at least once.",
		}),
		lockHeld: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace, Name: "collector_lock_held",
			Help: "1 while this process holds the collector lock and collects, 0 while another does.",
		}),
		deliveryReads: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "collector_delivery_reads_total",
			Help: "Root reads webhook deliveries asked for, by kind: caused (read because a delivery named the " +
				"root) or coalesced (due anyway in that cycle, and read once).",
		}, []string{labelKind}),
		deliveryCycles: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricNamespace, Name: "collector_delivery_cycles_total",
			Help: "Collection cycles a webhook delivery started before the interval ended.",
		}),
	}
	m.registerGitHub()
	reg.MustRegister(m.lastSuccess, m.readErrors, m.refusals, m.readDuration, m.written, m.storeErrors, m.cycles, m.roots,
		m.lockHeld, m.deliveryReads, m.deliveryCycles, m.githubReads, m.githubRequests, m.githubNotModified,
		m.githubRemaining)
	// Every series exists from the start, so that a rate over it is
	// defined before the first delivery.
	for _, kind := range []string{deliveryCaused, deliveryCoalesced} {
		m.deliveryReads.WithLabelValues(kind)
	}
	for _, kind := range githubTriggers {
		m.githubReads.WithLabelValues(kind)
	}
	return m
}

// registerGitHub makes the metrics of the roots' GitHub sides.
func (m *Metrics) registerGitHub() {
	m.githubReads = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricNamespace, Name: "collector_github_reads_total",
		Help: "Root reads' GitHub sides, by kind: read for a delivery, a run (a new root or changed runs) or a task " +
			"change; by the backstop, whole (backstop) or found unchanged from 304s (unchanged); or held back by " +
			"the rate limit's reserve (deferred).",
	}, []string{labelKind})
	m.githubRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace, Name: "github_requests_total", Help: "Requests the collector sent to GitHub.",
	})
	m.githubNotModified = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace, Name: "github_not_modified_total",
		Help: "Requests to GitHub answered 304 Not Modified, which its rate limit does not count.",
	})
	m.githubRemaining = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace, Name: "github_rate_limit_remaining",
		Help: "Requests left in GitHub's rate-limit window, as its last response said.",
	})
}

// observeRead records one read of a source instance that started at
// start and ended at end. A read the shutdown canceled is not an error.
func (m *Metrics) observeRead(src model.Source, instance string, start, end time.Time, err error) {
	m.readDuration.WithLabelValues(string(src), instance).Observe(end.Sub(start).Seconds())
	switch reason := reasonOf(err); {
	case err == nil:
		m.lastSuccess.WithLabelValues(string(src), instance).Set(float64(start.Unix()))
	case reason != "":
		m.readErrors.WithLabelValues(string(src), instance, reason).Inc()
	}
}

// observeRefusal counts the requests of r. Its resource, "check runs",
// is labeled check_runs.
func (m *Metrics) observeRefusal(r collector.Refusal) {
	resource := strings.ReplaceAll(r.Resource, " ", "_")
	m.refusals.WithLabelValues(string(r.Source), r.Instance, resource, strconv.Itoa(r.Code)).Add(float64(r.Count))
}

// observeWritten counts the records of a commit.
func (m *Metrics) observeWritten(recs model.Records) {
	m.written.WithLabelValues(kindEvents).Add(float64(len(recs.Events)))
	m.written.WithLabelValues(kindRuns).Add(float64(len(recs.Runs)))
	m.written.WithLabelValues(kindPullRequests).Add(float64(len(recs.PullRequests)))
	m.written.WithLabelValues(kindCorrelations).Add(float64(len(recs.Correlations)))
}
