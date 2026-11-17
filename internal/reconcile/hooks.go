package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// HookLog is the delivery log of sillage's own GitHub webhooks:
// *github.Client.
type HookLog interface {
	// HookDeliveries returns the hook's attempts newer than after and
	// delivered at or after since, newest first.
	HookDeliveries(ctx context.Context, hook github.Hook, after int64, since time.Time) ([]github.HookDelivery, error)
	// Redeliver asks GitHub to send an attempt again; github.Refused says
	// GitHub will not.
	Redeliver(ctx context.Context, hook github.Hook, id int64) error
	// HookPayload returns an attempt's event and payload.
	HookPayload(ctx context.Context, hook github.Hook, id int64) (event string, payload []byte, err error)
	// HookURL returns the URL the hook sends its deliveries to.
	HookURL(ctx context.Context, hook github.Hook) (string, error)
}

// Receipts are what sillage knows of the GitHub deliveries it received:
// cmd/sillage's, over the store and the ingest path's reading of a
// payload.
type Receipts interface {
	// Accepted reports whether an attempt of the GitHub delivery guid (its
	// X-GitHub-Delivery) was accepted.
	Accepted(ctx context.Context, guid string) (bool, error)
	// Roots returns the roots a GitHub delivery of event with payload
	// names, as the ingest path reads them.
	Roots(ctx context.Context, event string, payload []byte) ([]model.Root, error)
	// Ours reports whether a webhook sending to hookURL sends to
	// sillage's ingest path.
	Ours(hookURL string) bool
}

// The outcomes of a delivery GitHub could not hand to sillage, as
// labelOutcome values.
const (
	// lostFailed: an attempt sillage did not answer with a 2xx, and of
	// whose delivery it accepted none.
	lostFailed = "failed"
	// lostRedelivered: GitHub was asked to send it again.
	lostRedelivered = "redelivered"
	// lostUnrecoverable: GitHub would not send it again, or it was a
	// redelivery already: the roots its payload names are read again, a
	// gap open on their GitHub coverage from when it was sent.
	lostUnrecoverable = "unrecoverable"
)

// hookPrefix keys a hook's mark, after the GitHub instance and the hook.
const hookPrefix = checkpointPrefix + "github-hooks/"

// hookMark is a hook's checkpoint: the newest attempt of its delivery log
// handled, every older one handled too.
type hookMark struct {
	Newest int64 `json:"newest"`
}

// hookKey is the checkpoint key of hook's mark on instance.
func hookKey(instance string, hook github.Hook) string {
	return hookPrefix + instance + "/" + hook.String()
}

// lostDelivery is the earliest delivery naming a root that GitHub could
// not hand to sillage nor send again.
type lostDelivery struct {
	at     time.Time
	reason string
}

// hooksOn reports whether the loop reads delivery logs.
func (l *Loop) hooksOn() bool {
	return len(l.cfg.GitHubHooks) > 0 && l.src.Hooks != nil && l.src.Receipts != nil && l.src.GitHub != nil
}

// checkHooks reads each hook's delivery log since its mark, at the first
// cycle and then once every HookCheckInterval, and handles every attempt
// sillage lost: it asks GitHub to send it again, or adds the roots it
// names to dues, with a gap. A hook not known to send to sillage is
// skipped, and none is read while a GitHub rate limit is waited out. It
// returns the marks it advanced, by key.
func (l *Loop) checkHooks(ctx context.Context, dues map[model.Ref]*due) map[string]int64 {
	marks := make(map[string]int64)
	now := l.now()
	if !l.hooksOn() || l.githubPaused() ||
		(!l.hooksChecked.IsZero() && now.Sub(l.hooksChecked) < l.cfg.HookCheckInterval) {
		return marks
	}
	l.hooksChecked = now
	for _, hook := range l.cfg.GitHubHooks {
		key := hookKey(l.ccfg.GitHubInstance, hook)
		if !l.ownHook(ctx, hook, key) {
			continue
		}
		if newest, ok := l.checkHook(ctx, hook, key, dues); ok {
			marks[key] = newest
		}
		if ctx.Err() != nil {
			return marks
		}
	}
	return marks
}

// ownHook reports whether hook sends its deliveries to sillage, asking
// GitHub the first time: a hook that does not, another receiver's whose
// id was configured by mistake, is never read nor redelivered to, and
// said so once. A failed read leaves it unknown until the next check.
func (l *Loop) ownHook(ctx context.Context, hook github.Hook, key string) bool {
	if ours, ok := l.hookOwners[key]; ok {
		return ours
	}
	m := l.meterFor(model.SourceGitHub, l.ccfg.GitHubInstance)
	hookURL, err := timed(ctx, m, func(ctx context.Context) (string, error) {
		return l.src.Hooks.HookURL(ctx, hook)
	})
	if err != nil {
		if ctx.Err() == nil {
			l.noteGitHubError(err)
			l.log.Warn("source read failed: the webhook's URL, its delivery log not read", "source",
				model.SourceGitHub, "instance", l.ccfg.GitHubInstance, "hook", hook, "reason", reasonOf(err),
				"error", err)
		}
		return false
	}
	ours := l.src.Receipts.Ours(hookURL)
	l.hookOwners[key] = ours
	if !ours {
		l.log.Error("webhook does not send to sillage: its delivery log is never read, nothing redelivered to it",
			"source", model.SourceGitHub, "instance", l.ccfg.GitHubInstance, "hook", hook, "path", urlPath(hookURL))
	}
	return ours
}

// urlPath is hookURL's path, which the log names instead of the URL in
// case its query holds a secret.
func urlPath(hookURL string) string {
	u, err := url.Parse(hookURL)
	if err != nil {
		return "(not a URL)"
	}
	return u.Path
}

// checkHook handles hook's attempts newer than its mark, oldest first,
// and returns the newest handled, and whether it is newer than the mark.
// It stops at the first it could not handle, to handle it again at the
// next check. A hook without a mark is read back a backstop interval:
// the backstop has caught up with what was lost before.
func (l *Loop) checkHook(ctx context.Context, hook github.Hook, key string, dues map[model.Ref]*due) (int64, bool) {
	after, known := l.hookMarks[key]
	var since time.Time
	if !known {
		since = l.now().Add(-l.cfg.BackstopInterval)
	}
	m := l.meterFor(model.SourceGitHub, l.ccfg.GitHubInstance)
	attempts, err := timed(ctx, m, func(ctx context.Context) ([]github.HookDelivery, error) {
		return l.src.Hooks.HookDeliveries(ctx, hook, after, since)
	})
	if err != nil {
		if ctx.Err() == nil {
			l.noteGitHubError(err)
			l.log.Warn("source read failed: the webhook's delivery log", "source", model.SourceGitHub,
				"instance", l.ccfg.GitHubInstance, "hook", hook, "reason", reasonOf(err), "error", err)
		}
		return after, false
	}
	newest := after
	for _, a := range slices.Backward(attempts) {
		if ctx.Err() != nil || !l.handleAttempt(ctx, hook, key, a, dues) {
			break
		}
		newest = a.ID
	}
	return newest, newest != after
}

// handleAttempt handles one attempt of hook's log, and reports whether it
// did: false leaves it to the next check.
func (l *Loop) handleAttempt(ctx context.Context, hook github.Hook, key string, a github.HookDelivery,
	dues map[model.Ref]*due) bool {
	if a.Received() {
		return true
	}
	accepted, err := l.src.Receipts.Accepted(ctx, a.GUID)
	if err != nil {
		l.receiptsFailed(ctx, hook, a, err)
		return false
	}
	if accepted {
		return true
	}
	if a.Redelivery {
		return l.reread(ctx, hook, key, a, "it was a redelivery already", dues)
	}
	m := l.meterFor(model.SourceGitHub, l.ccfg.GitHubInstance)
	_, err = timed(ctx, m, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, l.src.Hooks.Redeliver(ctx, hook, a.ID)
	})
	switch {
	case err == nil:
		l.countLost(lostFailed, lostRedelivered)
		l.log.Info("lost GitHub delivery redelivered", attemptAttrs(hook, a)...)
		return true
	case github.Refused(err):
		return l.reread(ctx, hook, key, a, "GitHub refused to redeliver it ("+refusal(err)+")", dues)
	}
	if ctx.Err() == nil {
		l.log.Warn("lost GitHub delivery not redelivered yet", append(attemptAttrs(hook, a),
			"reason", reasonOf(err), "error", err)...)
	}
	return false
}

// reread adds the roots attempt a's payload names to dues, each with a
// gap on its GitHub coverage from when it was sent, because why GitHub
// will not send it again. A payload GitHub no longer has names none.
func (l *Loop) reread(ctx context.Context, hook github.Hook, key string, a github.HookDelivery, why string,
	dues map[model.Ref]*due) bool {
	m := l.meterFor(model.SourceGitHub, l.ccfg.GitHubInstance)
	type sent struct {
		event   string
		payload []byte
	}
	p, err := timed(ctx, m, func(ctx context.Context) (sent, error) {
		event, payload, err := l.src.Hooks.HookPayload(ctx, hook, a.ID)
		return sent{event, payload}, err
	})
	var roots []model.Root
	switch {
	case err == nil:
		if roots, err = l.src.Receipts.Roots(ctx, p.event, p.payload); err != nil {
			l.receiptsFailed(ctx, hook, a, err)
			return false
		}
	case !github.Refused(err):
		if ctx.Err() == nil {
			l.log.Warn("lost GitHub delivery's payload not read yet", append(attemptAttrs(hook, a),
				"reason", reasonOf(err), "error", err)...)
		}
		return false
	}
	l.countLost(lostFailed, lostUnrecoverable)
	l.log.Warn("lost GitHub delivery not redelivered: the roots it names are read again, with a gap",
		append(attemptAttrs(hook, a), "why", why, "roots", len(roots))...)
	lost := lostDelivery{at: a.DeliveredAt, reason: lostReason(a, why)}
	for _, root := range roots {
		l.addLost(dues, root, key, lost)
	}
	return true
}

// addLost adds root to dues, its GitHub side read whole, with lost's gap
// unless an earlier delivery's is there, and hook key's mark waiting for
// its commit.
func (l *Loop) addLost(dues map[model.Ref]*due, root model.Root, key string, lost lostDelivery) {
	ref := root.Ref()
	d, ok := dues[ref]
	if !ok {
		d = &due{root: root}
		dues[ref] = d
	}
	d.readGitHub(triggerLost)
	if d.lost == nil || lost.at.Before(d.lost.at) {
		d.lost = &lost
	}
	if !slices.Contains(d.hooks, key) {
		d.hooks = append(d.hooks, key)
	}
}

// lostReason is the reason of the gap attempt a opens, as a's delivery
// was not redelivered because why.
func lostReason(a github.HookDelivery, why string) string {
	answer := "no answer"
	if a.StatusCode != 0 {
		answer = "status " + strconv.Itoa(a.StatusCode)
	}
	return fmt.Sprintf("GitHub could not deliver a %s webhook delivery to sillage at %s (%s), and %s: "+
		"what GitHub showed from then until the next read, and no longer shows, is unknown",
		a.Event, a.DeliveredAt.UTC().Format(time.RFC3339), answer, why)
}

// refusal is the status GitHub refused a request with.
func refusal(err error) string {
	if se, ok := errors.AsType[*github.StatusError](err); ok {
		return se.Status
	}
	return err.Error()
}

// receiptsFailed reports that what sillage received of attempt a's
// delivery could not be read, unless the shutdown caused it.
func (l *Loop) receiptsFailed(ctx context.Context, hook github.Hook, a github.HookDelivery, err error) {
	if ctx.Err() != nil {
		return
	}
	l.metrics.storeErrors.WithLabelValues(operationRead).Inc()
	l.log.Error("store read failed: the receipts of a GitHub delivery", append(attemptAttrs(hook, a),
		"error", err)...)
}

// attemptAttrs are the log attributes of attempt a of hook.
func attemptAttrs(hook github.Hook, a github.HookDelivery) []any {
	return []any{"hook", hook, "delivery", a.GUID, "attempt", a.ID, "event", a.Event, "status", a.StatusCode,
		"deliveredAt", a.DeliveredAt}
}

// countLost counts a lost delivery once for each outcome.
func (l *Loop) countLost(outcomes ...string) {
	for _, o := range outcomes {
		l.metrics.lostDeliveries.WithLabelValues(o).Inc()
	}
}

// loseDelivery opens the gap d's lost delivery says on next's GitHub
// coverage, from when it was sent, unless a read since saw what it said
// or a gap is open already.
func (l *Loop) loseDelivery(next *rootState, d *due) {
	if d.lost == nil || l.src.GitHub == nil {
		return
	}
	st := next.source(model.Coverage{Source: model.SourceGitHub, Instance: l.ccfg.GitHubInstance})
	if st.LastSuccess != nil && st.LastSuccess.Before(d.lost.at) {
		st.openAfter(d.lost.at, d.lost.reason)
	}
}

// loadHookMark keeps the mark of the hook key names.
func (l *Loop) loadHookMark(key string, data []byte) error {
	var mark hookMark
	if err := json.Unmarshal(data, &mark); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	l.hookMarks[key] = mark.Newest
	return nil
}

// hookCheckpoints adds to cps the marks of the hooks whose lost
// deliveries' roots were all committed, and returns them.
func (l *Loop) hookCheckpoints(cps map[string]Checkpoint, marks map[string]int64, incomplete map[string]bool) (
	advanced map[string]int64) {
	advanced = make(map[string]int64)
	for key, newest := range marks {
		if incomplete[key] {
			continue
		}
		cps[key] = Checkpoint{
			Source: model.SourceGitHub, Instance: l.ccfg.GitHubInstance, Value: encode(hookMark{Newest: newest}),
		}
		advanced[key] = newest
	}
	return advanced
}
