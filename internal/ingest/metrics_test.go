package ingest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	const series = 2 * 6 // two sources, six outcomes
	if got := testutil.CollectAndCount(m.deliveries); got != series {
		t.Errorf("%d series, want %d from the start", got, series)
	}
	if _, err := NewMetrics(reg); err == nil {
		t.Error("registering twice succeeded")
	}
	if _, err := NewMetrics(nil); err == nil {
		t.Error("NewMetrics(nil) succeeded")
	}
}
