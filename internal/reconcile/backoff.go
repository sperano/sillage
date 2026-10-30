package reconcile

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// maxBackoff caps how long a root whose reads keep failing to be saved is
// left unread.
const maxBackoff = 4 * time.Hour

// backoff is how long a root whose read could not be saved (its stored
// records not read, its story not assembled, or its commit refused) is
// left unread. Its state is not saved, so that without one the root would
// be due again at the next cycle, every source read again, GitHub
// included. It is kept in memory only: a restart reads every root again
// anyway.
type backoff struct {
	root     model.Root
	failures int
	until    time.Time
	// err is the last failure's, which the log names once per change.
	err string
	// delivered is whether a delivery asked for a read the back-off
	// skipped: the read that follows it is the delivery's.
	delivered bool
}

// backingOff reports whether d's root is backing off, and so is not read.
// A delivery's ask is kept for the read that follows.
func (l *Loop) backingOff(d *due) bool {
	b := l.backoffs[d.root.Ref()]
	if b == nil || !l.now().Before(b.until) {
		return false
	}
	if d.delivery != "" {
		b.delivered = true
	}
	return true
}

// backOff has root wait before it is read again, since its read could
// not be saved for err: a cycle interval after the first failure, twice
// as long after each that follows, at most maxBackoff. delivered is
// whether a delivery asked for the read. It logs the root and the error
// when the back-off starts, and when the error changes.
func (l *Loop) backOff(root model.Root, err error, delivered bool) {
	b := l.backoffs[root.Ref()]
	if b == nil {
		b = &backoff{root: root}
		l.backoffs[root.Ref()] = b
	}
	b.failures++
	wait := backoffWait(l.cfg.Interval, b.failures)
	b.until = l.now().Add(wait)
	b.delivered = b.delivered || delivered
	if msg := err.Error(); msg != b.err {
		b.err = msg
		l.log.Error("root read not saved: backing off before reading it again", "root", root.Ref(),
			"failures", b.failures, "wait", wait, "error", err)
	}
	l.metrics.backoffRoots.Set(float64(len(l.backoffs)))
}

// backoffWait is the wait after the failures-th failure in a row: base,
// doubled per failure after the first, at most maxBackoff.
func backoffWait(base time.Duration, failures int) time.Duration {
	wait := min(base, maxBackoff)
	for range failures - 1 {
		if wait >= maxBackoff/2 {
			return maxBackoff
		}
		wait *= 2
	}
	return wait
}

// recovered ends root's back-off, its read saved, and says so.
func (l *Loop) recovered(root model.Ref) {
	b, ok := l.backoffs[root]
	if !ok {
		return
	}
	delete(l.backoffs, root)
	l.metrics.backoffRoots.Set(float64(len(l.backoffs)))
	l.log.Info("root read saved again: its back-off is over", "root", root, "failures", b.failures)
}

// deliveredAfterBackoff returns the roots whose back-off is over and that
// a delivery asked to read meanwhile, and forgets that it did: their next
// read is the delivery's.
func (l *Loop) deliveredAfterBackoff() []model.Root {
	var roots []model.Root
	now := l.now()
	for _, b := range l.backoffs {
		if b.delivered && !now.Before(b.until) {
			b.delivered = false
			roots = append(roots, b.root)
		}
	}
	return roots
}
