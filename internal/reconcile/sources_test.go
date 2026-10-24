package reconcile

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/wiki"
)

// countingWiki lists pages, failing with err, and counts the listings.
type countingWiki struct {
	noWiki
	err      error
	listings int
}

func (w *countingWiki) Pages(ctx context.Context) ([]wiki.Page, error) {
	w.listings++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, w.err
}

func testMeter(source model.Source) meter {
	return meter{m: NewMetrics(prometheus.NewRegistry()), now: time.Now, source: source, instance: testWikiHost}
}

func TestCycleWikiListsOnce(t *testing.T) {
	down := errors.New("fabricated wiki outage")
	src := &countingWiki{err: down}
	w := &cycleWiki{src: src, meter: testMeter(model.SourceWiki)}
	for range 3 {
		if _, err := w.Pages(t.Context()); !errors.Is(err, down) {
			t.Fatalf("Pages() error = %v, want the outage", err)
		}
	}
	if src.listings != 1 {
		t.Errorf("listed %d times in a cycle, want once", src.listings)
	}
}

func TestCycleWikiDoesNotKeepARootsTimeout(t *testing.T) {
	src := &countingWiki{}
	w := &cycleWiki{src: src, meter: testMeter(model.SourceWiki)}
	expired, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	if _, err := w.Pages(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Pages() error = %v, want the root's deadline", err)
	}
	if _, err := w.Pages(t.Context()); err != nil {
		t.Errorf("Pages() for the next root = %v, want a listing of its own", err)
	}
	if src.listings != 2 {
		t.Errorf("listed %d times, want again after the timeout", src.listings)
	}
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
	_, err := New(everyCycle, testCollectorConfig(), SourcesOf(collector.Clients{}), newFakeStore(),
		NewMetrics(prometheus.NewRegistry()), slog.New(slog.DiscardHandler))
	if err == nil {
		t.Error("New() over unset clients = nil error, want them refused")
	}
}
