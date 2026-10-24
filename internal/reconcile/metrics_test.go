package reconcile

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
)

// readCount is how many reads of src's instance the duration histogram
// observed.
func readCount(t *testing.T, reg prometheus.Gatherer, src model.Source, instance string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "sillage_source_read_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := make(map[string]string)
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels[labelSource] == string(src) && labels[labelInstance] == instance {
				return m.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func TestMetricsAfterACycle(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.vikunja.set(errUnavailable)
	h.loop.cycle(t.Context())
	h.vikunja.set(nil)
	h.loop.cycle(t.Context())
	m := h.loop.metrics
	vikunja, kelos := string(model.SourceVikunja), string(model.SourceKelos)
	if got := testutil.ToFloat64(m.readErrors.WithLabelValues(vikunja, testVikunjaHost, reasonServerError)); got != 1 {
		t.Errorf("vikunja server errors = %v, want the one failed read", got)
	}
	if got := testutil.CollectAndCount(m.readErrors); got != 1 {
		t.Errorf("read error series = %d, want only vikunja's server error", got)
	}
	for _, src := range []struct{ source, instance string }{{vikunja, testVikunjaHost}, {kelos, testCluster}} {
		if got := testutil.ToFloat64(m.lastSuccess.WithLabelValues(src.source, src.instance)); got <= 0 {
			t.Errorf("%s last success = %v, want the second cycle's read", src.source, got)
		}
	}
	if got := readCount(t, h.reg, model.SourceVikunja, testVikunjaHost); got != 2 {
		t.Errorf("vikunja reads timed = %d, want both, failed or not", got)
	}
	if got := testutil.ToFloat64(m.written.WithLabelValues(kindEvents)); got <= 0 {
		t.Errorf("events written = %v, want the read story's", got)
	}
	if got := testutil.ToFloat64(m.written.WithLabelValues(kindRuns)); got != 1+1 {
		t.Errorf("runs written = %v, want the run at each of the two commits", got)
	}
	if got := testutil.ToFloat64(m.cycles); got != 2 {
		t.Errorf("cycles = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.roots); got != 1 {
		t.Errorf("roots = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(m.storeErrors); got != 0 {
		t.Errorf("store error series = %d, want none", got)
	}
}
