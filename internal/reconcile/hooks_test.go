package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// testHook is sillage's fabricated webhook on testRepo.
var testHook = github.Hook{Repository: testRepo, ID: 9001}

// errForbidden is GitHub refusing a redelivery to a credential without
// webhook write access.
var errForbidden = &github.StatusError{
	Method: http.MethodPost, Path: "/repos/acme/widgets/hooks/9001/deliveries/1/attempts",
	Code: http.StatusForbidden, Status: "403 Forbidden",
}

// sillageHookURL is where testHook sends its deliveries, unless a test
// says otherwise.
const sillageHookURL = "https://sillage.example.com/ingest/github"

// errBadGateway is GitHub failing a request, which asking again may not.
var errBadGateway = &github.StatusError{Path: "/repos/acme/widgets/hooks/9001/deliveries", Code: http.StatusBadGateway,
	Status: "502 Bad Gateway"}

// fakeHookLog is testHook's delivery log, newest first, as GitHub keeps
// it; each attempt's payload names the task its id is keyed with in
// tasks.
type fakeHookLog struct {
	mu       sync.Mutex
	attempts []github.HookDelivery
	tasks    map[int64]int64
	// redeliverErr answers every redelivery while set.
	redeliverErr error
	// url is where the hook sends, sillageHookURL when empty; urlErr
	// answers every read of it while set, and urlReads counts them.
	url      string
	urlErr   error
	urlReads int
	// asked are the marks each read of the log was asked from, since the
	// times; redelivered the attempts GitHub was asked to send again.
	asked       []int64
	since       []time.Time
	redelivered []int64
}

// fail appends an attempt GitHub could not deliver at at, of the
// delivery guid, its payload naming task.
func (f *fakeHookLog) fail(id int64, guid string, at time.Time, task int64, redelivery bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := github.HookDelivery{ID: id, GUID: guid, DeliveredAt: at, StatusCode: http.StatusBadGateway,
		Event: "pull_request", Redelivery: redelivery}
	f.attempts = slices.Insert(f.attempts, 0, a)
	if f.tasks == nil {
		f.tasks = make(map[int64]int64)
	}
	f.tasks[id] = task
}

// deliver appends an attempt sillage received.
func (f *fakeHookLog) deliver(id int64, guid string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = slices.Insert(f.attempts, 0, github.HookDelivery{ID: id, GUID: guid, DeliveredAt: at,
		StatusCode: http.StatusAccepted, Event: "pull_request"})
}

func (f *fakeHookLog) HookDeliveries(_ context.Context, hook github.Hook, after int64, since time.Time) (
	[]github.HookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if hook != testHook {
		return nil, errors.New("unknown hook " + hook.String())
	}
	f.asked, f.since = append(f.asked, after), append(f.since, since)
	var out []github.HookDelivery
	for _, a := range f.attempts {
		if a.ID <= after || a.DeliveredAt.Before(since) {
			break
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeHookLog) Redeliver(_ context.Context, _ github.Hook, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.redeliverErr != nil {
		return f.redeliverErr
	}
	f.redelivered = append(f.redelivered, id)
	return nil
}

func (f *fakeHookLog) HookPayload(_ context.Context, _ github.Hook, id int64) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[id]
	if !ok {
		return "", nil, &github.StatusError{Path: "/deliveries/" + strconv.FormatInt(id, 10), Code: http.StatusNotFound,
			Status: "404 Not Found"}
	}
	return "pull_request", []byte(strconv.FormatInt(task, 10)), nil
}

func (f *fakeHookLog) HookURL(_ context.Context, _ github.Hook) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urlReads++
	if f.urlErr != nil {
		return "", f.urlErr
	}
	if f.url == "" {
		return sillageHookURL, nil
	}
	return f.url, nil
}

func (f *fakeHookLog) setURLErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urlErr = err
}

func (f *fakeHookLog) urlsRead() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.urlReads
}

func (f *fakeHookLog) setRedeliverErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.redeliverErr = err
}

func (f *fakeHookLog) redeliveries() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.redelivered)
}

func (f *fakeHookLog) marks() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

// fakeReceipts are the deliveries sillage accepted, by guid; a payload
// is the id of the task it names.
type fakeReceipts struct {
	mu       sync.Mutex
	accepted map[string]bool
}

func (r *fakeReceipts) accept(guid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.accepted == nil {
		r.accepted = make(map[string]bool)
	}
	r.accepted[guid] = true
}

func (r *fakeReceipts) Accepted(_ context.Context, guid string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accepted[guid], nil
}

func (r *fakeReceipts) Roots(_ context.Context, _ string, payload []byte) ([]model.Root, error) {
	var task int64
	if err := json.Unmarshal(payload, &task); err != nil {
		return nil, err
	}
	return []model.Root{taskRoot(task)}, nil
}

func (r *fakeReceipts) Ours(hookURL string) bool { return hookURL == sillageHookURL }

// hookHarness is a githubHarness whose loop reads testHook's log.
type hookHarness struct {
	*harness
	gh       *conditionalGitHub
	log      *fakeHookLog
	receipts *fakeReceipts
	cfg      Config
}

// rarelyChecked reads the hooks' logs at every cycle, and refreshes
// nothing unless a test says so.
func rarelyChecked() Config {
	cfg := rarely
	cfg.GitHubHooks, cfg.HookCheckInterval = []github.Hook{testHook}, time.Nanosecond
	return cfg
}

func newHookHarness(t *testing.T, cfg Config) *hookHarness {
	t.Helper()
	h, gh := githubHarness(t, cfg)
	hh := &hookHarness{harness: h, gh: gh, log: &fakeHookLog{}, receipts: &fakeReceipts{}, cfg: cfg}
	hh.restart(t)
	return hh
}

// restart starts another loop over the same fakes and store.
func (h *hookHarness) restart(t *testing.T) {
	t.Helper()
	src := h.sources()
	src.GitHub, src.Hooks, src.Receipts = h.gh, h.log, h.receipts
	h.loop = h.loopOf(t, h.cfg, h.ccfg, src)
}

func (h *hookHarness) lost(outcome string) float64 {
	return testutil.ToFloat64(h.loop.metrics.lostDeliveries.WithLabelValues(outcome))
}

// githubCoverage is the stored GitHub coverage of root.
func (h *hookHarness) githubCoverage(t *testing.T, root model.Root) model.Coverage {
	t.Helper()
	for _, cov := range h.store.story(t, root).Coverage {
		if cov.Source == model.SourceGitHub {
			return cov
		}
	}
	t.Fatal("no GitHub coverage")
	return model.Coverage{}
}

func TestHookLogIsReadSinceItsMark(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.log.deliver(101, "guid-101", time.Now())
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	asked := h.log.marks()
	if len(asked) != 2 || asked[0] != 0 || asked[1] != 101 {
		t.Fatalf("the log was read from %v, want from nothing, then from its newest attempt", asked)
	}
	if since := h.log.since[0]; time.Since(since) < rarely.BackstopInterval-time.Minute {
		t.Errorf("a hook without a mark is read back to %v, want a backstop interval", since)
	}
	if !h.log.since[1].IsZero() {
		t.Errorf("a hook with a mark is read back to %v, want to its mark alone", h.log.since[1])
	}
	h.restart(t)
	h.loop.cycle(t.Context())
	if asked := h.log.marks(); asked[len(asked)-1] != 101 {
		t.Errorf("after a restart the log was read from %d, want the stored mark 101", asked[len(asked)-1])
	}
}

func TestHookLogIsReadOncePerCheckInterval(t *testing.T) {
	cfg := rarelyChecked()
	cfg.HookCheckInterval = time.Hour
	h := newHookHarness(t, cfg)
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if n := len(h.log.marks()); n != 1 {
		t.Errorf("the log was read %d times in two cycles, want once: at the first, then an hour later", n)
	}
}

func TestAcceptedDeliveryIsNotRedelivered(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.loop.cycle(t.Context())
	// sillage accepted it, but its answer did not reach GitHub in time.
	h.receipts.accept("guid-102")
	h.log.fail(102, "guid-102", time.Now(), testTaskID, false)
	h.loop.cycle(t.Context())
	if got := h.log.redeliveries(); len(got) != 0 {
		t.Errorf("redelivered %v, want nothing: the delivery's guid was accepted", got)
	}
	if got := h.lost(lostFailed); got != 0 {
		t.Errorf("lost deliveries = %v, want 0", got)
	}
}

func TestLostDeliveryIsRedeliveredOnce(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.loop.cycle(t.Context())
	h.log.fail(103, "guid-103", time.Now(), testTaskID, false)
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if got := h.log.redeliveries(); len(got) != 1 || got[0] != 103 {
		t.Errorf("redelivered %v, want attempt 103 once", got)
	}
	if failed, redelivered := h.lost(lostFailed), h.lost(lostRedelivered); failed != 1 || redelivered != 1 {
		t.Errorf("lost deliveries: failed %v, redelivered %v; want 1 and 1", failed, redelivered)
	}
	if n := strings.Count(h.logs.String(), "lost GitHub delivery redelivered"); n != 1 {
		t.Errorf("the redelivery was logged %d times, want once:\n%s", n, h.logs)
	}
	if cov := h.githubCoverage(t, testRoot()); len(gapsWith(cov, "could not deliver")) != 0 {
		t.Errorf("a redelivered delivery left a gap: %+v", cov.Gaps)
	}
}

func TestRefusedRedeliveryRereadsItsRootsWithAGap(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.loop.cycle(t.Context())
	fetches, _ := h.gh.counts()
	h.log.setRedeliverErr(errForbidden)
	lostAt := time.Now().UTC()
	h.log.fail(104, "guid-104", lostAt, testTaskID, false)
	h.loop.cycle(t.Context())
	if now, _ := h.gh.counts(); now != fetches+1 {
		t.Errorf("GitHub read whole %d times, want once more for the root the lost delivery names", now-fetches)
	}
	gaps := gapsWith(h.githubCoverage(t, testRoot()), "403 Forbidden")
	if len(gaps) != 1 || !gaps[0].Start.Equal(lostAt) || !gaps[0].End.After(lostAt) {
		t.Fatalf("gaps %+v, want one from the lost delivery to the read", gaps)
	}
	if failed, lost := h.lost(lostFailed), h.lost(lostUnrecoverable); failed != 1 || lost != 1 {
		t.Errorf("lost deliveries: failed %v, unrecoverable %v; want 1 and 1", failed, lost)
	}
	if got := testutil.ToFloat64(h.loop.metrics.githubReads.WithLabelValues(triggerLost)); got != 1 {
		t.Errorf("GitHub reads for a lost delivery = %v, want 1", got)
	}
}

func TestFailedRedeliveryIsNotRedeliveredAgain(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.vikunja.also = []int64{testTaskID + 1}
	h.loop.cycle(t.Context())
	h.log.fail(105, "guid-105", time.Now(), testTaskID+1, true)
	h.loop.cycle(t.Context())
	if got := h.log.redeliveries(); len(got) != 0 {
		t.Errorf("redelivered %v, want nothing: it was a redelivery already", got)
	}
	if s := h.store.story(t, taskRoot(testTaskID+1)); len(s.Events) == 0 {
		t.Errorf("the task the lost redelivery names was not read: %+v", s.Records)
	}
	if got := h.lost(lostUnrecoverable); got != 1 {
		t.Errorf("unrecoverable deliveries = %v, want 1", got)
	}
}

func TestUnhandledAttemptIsHandledAtTheNextCheck(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.loop.cycle(t.Context())
	h.log.setRedeliverErr(errBadGateway)
	h.log.deliver(106, "guid-106", time.Now())
	h.log.fail(107, "guid-107", time.Now(), testTaskID, false)
	h.log.fail(108, "guid-108", time.Now(), testTaskID, false)
	h.loop.cycle(t.Context())
	h.log.setRedeliverErr(nil)
	h.loop.cycle(t.Context())
	if asked := h.log.marks(); asked[len(asked)-1] != 106 {
		t.Errorf("the log was read again from %d, want 106: 107 was not handled", asked[len(asked)-1])
	}
	if got := h.log.redeliveries(); len(got) != 2 || got[0] != 107 || got[1] != 108 {
		t.Errorf("redelivered %v, want 107 and 108, oldest first", got)
	}
}

func TestHooksAreNotReadWithoutReceipts(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	src := h.sources()
	src.GitHub, src.Hooks = h.gh, h.log
	h.loop = h.loopOf(t, h.cfg, h.ccfg, src)
	h.loop.cycle(t.Context())
	if asked := h.log.marks(); len(asked) != 0 {
		t.Errorf("the log was read without receipts to match it against: %v", asked)
	}
}

func TestHookURLIsReadOnce(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if n := h.log.urlsRead(); n != 1 {
		t.Errorf("the hook's URL was read %d times in two checks, want once", n)
	}
	if n := len(h.log.marks()); n != 2 {
		t.Errorf("the log was read %d times in two checks, want twice", n)
	}
}

// A hook id that names another receiver's webhook, the spawner's, is
// never redelivered to: nothing of its log is about sillage.
func TestHookNotSillagesIsNeverRedeliveredTo(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.log.url = "https://spawner.example.com/webhook/github"
	h.log.fail(105, "guid-105", time.Now(), testTaskID, false)
	h.loop.cycle(t.Context())
	h.loop.cycle(t.Context())
	if got := h.log.redeliveries(); len(got) != 0 {
		t.Errorf("redelivered %v to a hook that does not send to sillage", got)
	}
	if asked := h.log.marks(); len(asked) != 0 {
		t.Errorf("the log of a hook that does not send to sillage was read: %v", asked)
	}
	if n := h.log.urlsRead(); n != 1 {
		t.Errorf("the hook's URL was read %d times, want once", n)
	}
}

func TestHookURLNotReadIsReadAtTheNextCheck(t *testing.T) {
	h := newHookHarness(t, rarelyChecked())
	h.log.setURLErr(errBadGateway)
	h.log.fail(105, "guid-105", time.Now(), testTaskID, false)
	h.loop.cycle(t.Context())
	if got := h.log.redeliveries(); len(got) != 0 || len(h.log.marks()) != 0 {
		t.Errorf("redelivered %v, log read %v, before the hook's URL was read", got, h.log.marks())
	}
	h.log.setURLErr(nil)
	h.loop.cycle(t.Context())
	if got := h.log.redeliveries(); len(got) != 1 || got[0] != 105 {
		t.Errorf("redelivered %v once the hook's URL was read, want 105", got)
	}
}
