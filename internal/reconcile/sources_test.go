package reconcile

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

func testMeter(source model.Source) meter {
	return meter{m: NewMetrics(prometheus.NewRegistry()), now: time.Now, source: source, instance: testGitHubHost}
}

func TestTimedBoundsEachRead(t *testing.T) {
	for range 2 {
		_, err := timed(t.Context(), testMeter(model.SourceGitHub), func(ctx context.Context) (struct{}, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > readTimeout {
				t.Errorf("read deadline = %v (set %v), want one within %v", deadline, ok, readTimeout)
			}
			return struct{}{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourcesOfUnsetClientsAreRefused(t *testing.T) {
	_, err := New(DefaultConfig(), testCollectorConfig(), SourcesOf(collector.Clients{}), newFakeStore(), nil,
		NewMetrics(prometheus.NewRegistry()), slog.New(slog.DiscardHandler))
	if err == nil {
		t.Error("New() over unset clients = nil error, want them refused")
	}
}
