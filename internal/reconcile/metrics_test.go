package reconcile

import (
	"slices"
	"testing"
)

func TestMetricsAfterACycle(t *testing.T) {
	h := newHarness(t, everyCycle)
	h.vikunja.set(errUnavailable)
	h.loop.cycle(t.Context())
	h.vikunja.set(nil)
	h.loop.cycle(t.Context())
	families, err := h.reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	for _, want := range []string{
		"sillage_source_last_success_timestamp_seconds", "sillage_source_read_errors_total",
		"sillage_source_read_duration_seconds", "sillage_records_written_total", "sillage_collector_cycles_total",
		"sillage_collector_roots",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("no %s among %v", want, names)
		}
	}
}
