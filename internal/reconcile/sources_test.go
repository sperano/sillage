package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

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
