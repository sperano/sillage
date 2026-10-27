// Package storytest holds the properties the fixture stories under
// testdata/fixtures/stories exist to protect, so that every producer of
// stories (the model's golden tests, the collector's) asserts the same
// ones. It is imported by tests only.
package storytest

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// invariants asserts, for each fixture story, the property it exists to
// protect. Every directory under testdata/fixtures/stories needs an entry.
// The collector's tests assert the same properties of the stories it
// assembles from source responses.
var invariants = map[string]func(*testing.T, model.Story){
	"discuss-no-pr":           checkDiscussNoPR,
	"implementation-merged":   checkImplementationMerged,
	"failed-run-retry":        checkFailedRunRetry,
	"announced-run-only":      checkAnnouncedRunOnly,
	"checks-after-force-push": checkChecksAfterForcePush,
	"reopened-task":           checkReopenedTask,
	"second-pr-after-close":   checkSecondPRAfterClose,
	"phase-markers":           checkPhaseMarkers,
}

// Names returns the fixture stories that have an invariant, sorted.
func Names() []string { return slices.Sorted(maps.Keys(invariants)) }

// Check asserts the invariant of fixture story name of s, failing t when
// name has none.
func Check(t *testing.T, name string, s model.Story) {
	t.Helper()
	check, ok := invariants[name]
	if !ok {
		t.Fatalf("story %s has no invariant", name)
	}
	check(t, s)
}

// checkDiscussNoPR: no pull request is invented and nothing implies a
// coding phase; the deliverables are outside the model and a reply.
func checkDiscussNoPR(t *testing.T, s model.Story) {
	if len(s.PullRequests) != 0 {
		t.Errorf("pull requests = %v, want none", s.PullRequests)
	}
	for _, e := range s.Events {
		if strings.HasPrefix(string(e.Kind), "pr.") || e.Related.PR != nil {
			t.Errorf("event %s implies a pull request", e.Key())
		}
	}
	for _, c := range s.Correlations {
		if c.From.Kind == model.RefPR || c.To.Kind == model.RefPR {
			t.Errorf("correlation %s involves a pull request", c)
		}
	}
	for _, r := range s.Runs {
		if r.Role != model.RoleDiscussion {
			t.Errorf("run %s has role %s, want %s", r.Key, r.Role, model.RoleDiscussion)
		}
	}
	if len(eventsByActorKind(s, model.KindComment, model.ActorAgent)) != 1 {
		t.Error("the agent's reply comment is missing")
	}
	if !hasCoverage(s, model.SourceGitHub) {
		t.Error("no GitHub coverage: no PR found must mean GitHub was read")
	}
}

// checkImplementationMerged: the merge and the task's completion stay two
// events; checks keep one result per commit; the PR's account and executor
// stay apart.
func checkImplementationMerged(t *testing.T, s model.Story) {
	merged := onlyEvent(t, s, model.KindPRMerged)
	done := onlyEvent(t, s, model.KindTaskDone)
	if merged.Source != model.SourceGitHub || done.Source != model.SourceVikunja {
		t.Errorf("merge from %s, done from %s; want github and vikunja", merged.Source, done.Source)
	}
	if !done.Time().After(merged.Time()) {
		t.Errorf("done at %v is not after the merge at %v", done.Time(), merged.Time())
	}
	if merged.Related.Task != nil || done.Related.PR != nil {
		t.Error("the merge names the task or the completion names the PR")
	}
	review := onlyEvent(t, s, model.KindReviewSubmitted)
	if !review.Time().Before(merged.Time()) {
		t.Error("the review is not before the merge")
	}

	if len(s.PullRequests) != 1 {
		t.Fatalf("pull requests = %d, want 1", len(s.PullRequests))
	}
	checks := s.PullRequests[0].Checks
	if shas := s.PullRequests[0].SHAs(); len(shas) != 2 {
		t.Fatalf("checked commits = %v, want 2", shas)
	}
	if checks[0].Conclusion != model.CheckFailure || checks[len(checks)-1].Conclusion != model.CheckSuccess {
		t.Errorf("checks = %v, want a failure on the first commit, then a success", checks)
	}

	opened := onlyEvent(t, s, model.KindPROpened)
	if opened.Actor.Kind != model.ActorPerson || opened.Executor == nil || opened.Executor.Kind != model.ActorAgent {
		t.Errorf("PR opened by %+v executed by %+v, want a person's account and an agent", opened.Actor, opened.Executor)
	}
}

// checkFailedRunRetry: a retry under the same Task name is a second run.
func checkFailedRunRetry(t *testing.T, s model.Story) {
	if len(s.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(s.Runs))
	}
	first, retry := s.Runs[0], s.Runs[1]
	if first.TaskName != retry.TaskName {
		t.Errorf("task names %q and %q, want the same", first.TaskName, retry.TaskName)
	}
	if first.Key == retry.Key || first.Key.UID == "" || retry.Key.UID == "" {
		t.Errorf("run keys %s and %s, want two distinct UIDs", first.Key, retry.Key)
	}
	if first.Outcome != model.OutcomeFailed || retry.Outcome != model.OutcomeSucceeded {
		t.Errorf("outcomes %s then %s, want failed then succeeded", first.Outcome, retry.Outcome)
	}
	announced := eventsOfKind(s, model.KindRunAnnounced)
	if len(announced) != 2 {
		t.Fatalf("announcements = %d, want 2", len(announced))
	}
	for i, e := range announced {
		runs := correlationsFrom(s, e.Ref(), model.RefRun)
		if len(runs) != 1 || runs[0].To != s.Runs[i].Key.Ref() {
			t.Errorf("announcement %s links %v, want only %s", e.Key(), runs, s.Runs[i].Key)
		}
	}
}

// checkAnnouncedRunOnly: an interval nothing recorded stays unknown, even
// with a later event that could tempt an inferred end.
func checkAnnouncedRunOnly(t *testing.T, s model.Story) {
	if len(s.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(s.Runs))
	}
	run := s.Runs[0]
	if run.Key.UID != "" || run.Key.Pod == "" {
		t.Errorf("run key %s, want one keyed by pod", run.Key)
	}
	if run.Interval.Start.State != model.BoundUnknown || run.Interval.End.State != model.BoundUnknown {
		t.Errorf("interval %+v, want both ends unknown", run.Interval)
	}
	if d, ok := run.Interval.Duration(); ok {
		t.Errorf("duration %v, want none", d)
	}
	if run.Outcome != model.OutcomeUnknown {
		t.Errorf("outcome %s, want unknown", run.Outcome)
	}
	if run.Usage != nil {
		t.Errorf("usage = %+v, want none without a TaskRecord", run.Usage)
	}
	if len(eventsByActorKind(s, model.KindComment, model.ActorAgent)) == 0 {
		t.Error("the fixture lost the agent's later comment it exists to resist")
	}
	announced := onlyEvent(t, s, model.KindRunAnnounced)
	kelos := coverageOf(s, model.SourceKelos)
	if kelos == nil || kelos.Known(announced.Time()) {
		t.Error("Kelos coverage does not mark the run's period unknown")
	}
	if kelos == nil || !slices.ContainsFunc(kelos.Gaps, func(g model.Gap) bool {
		return strings.Contains(g.Reason, "TaskRecord") && strings.Contains(g.Reason, "reported usage")
	}) {
		t.Error("Kelos coverage does not explain that a run without reported usage leaves no TaskRecord after the TTL")
	}
}

// checkChecksAfterForcePush: check results from every observed head stay in
// the story after the branch moves to a new, non-descendant head.
func checkChecksAfterForcePush(t *testing.T, s model.Story) {
	if len(s.PullRequests) != 1 {
		t.Fatalf("pull requests = %d, want 1", len(s.PullRequests))
	}
	pr := s.PullRequests[0]
	if shas := pr.SHAs(); len(shas) != 3 {
		t.Fatalf("checked SHAs = %v, want all three heads around the force push", shas)
	}
	want := map[model.CheckConclusion]bool{model.CheckFailure: true, model.CheckSuccess: true}
	for _, check := range pr.Checks {
		delete(want, check.Conclusion)
	}
	if len(want) != 0 {
		t.Errorf("checks = %v, want failures and successes retained", pr.Checks)
	}
}

// checkReopenedTask: a final Vikunja read exposes only the latest done_at;
// the story states that source limitation instead of fabricating the first
// completion or the reopen. The read saw the task done, and no read saw it
// not done after a completion: it reads done, not reopened.
func checkReopenedTask(t *testing.T, s model.Story) {
	done := eventsOfKind(s, model.KindTaskDone)
	if len(done) != 1 {
		t.Fatalf("done events = %d, want only Vikunja's latest completion", len(done))
	}
	if !strings.Contains(done[0].Evidence.Note, "does not retain earlier completion or reopen times") {
		t.Errorf("done evidence = %q, want the source limitation", done[0].Evidence.Note)
	}
	seen := eventsOfKind(s, model.KindTaskCompletionObserved)
	if len(seen) != 1 || seen[0].Details[model.DetailDone] != model.DoneTrue {
		t.Errorf("completion observations = %v, want the one read, seeing it done", seen)
	}
	c, ok := s.CompletionAt(done[0].ObservedAt.Add(time.Nanosecond))
	if !ok || c.State != model.CompletionDone || c.Reopened != nil {
		t.Errorf("completion after the read = %+v, want done: no read saw the reopening", c)
	}
}

// checkSecondPRAfterClose: closing an unmerged pull request is not task
// completion; a later pull request on the same branch remains part of the
// open task's story.
func checkSecondPRAfterClose(t *testing.T, s model.Story) {
	if len(s.PullRequests) != 2 {
		t.Fatalf("pull requests = %d, want 2", len(s.PullRequests))
	}
	first, second := s.PullRequests[0], s.PullRequests[1]
	if first.Branch != second.Branch {
		t.Errorf("branches = %q and %q, want the same branch", first.Branch, second.Branch)
	}
	closed := eventsForPR(s, model.KindPRClosed, first.Key)
	merged := eventsForPR(s, model.KindPRMerged, first.Key)
	opened := eventsForPR(s, model.KindPROpened, second.Key)
	if len(closed) != 1 || len(merged) != 0 || len(opened) != 1 {
		t.Errorf("first close=%d merge=%d, second open=%d; want 1, 0, 1", len(closed), len(merged), len(opened))
	}
	if len(closed) == 1 && len(opened) == 1 && !opened[0].Time().After(closed[0].Time()) {
		t.Error("the second pull request did not open after the first closed")
	}
	if len(eventsOfKind(s, model.KindTaskDone)) != 0 {
		t.Error("the unmerged close invented task completion")
	}
}

// phasesWant are the phases of phase-markers' failed attempt and of its
// retry, in order: each state, and its edges' kinds ("" for none). The
// retry's first agent span is its Job's first pod's.
var phasesWant = [][]struct {
	state      model.PhaseState
	start, end model.MarkerKind
}{
	{
		{model.PhaseComplete, model.MarkerBlockerWait, model.MarkerBlockerWait},
		{model.PhaseInterrupted, model.MarkerAgent, ""},
		{model.PhaseStartNotRecorded, "", model.MarkerAgent},
	},
	{
		{model.PhaseInterrupted, model.MarkerAgent, ""},
		{model.PhaseInProgress, model.MarkerAgent, ""},
	},
}

// checkPhaseMarkers: a phase is never inferred from a pod or a gap, nor
// closed at the run's end; each recorded edge stays, and an inconsistent
// pair has no interval; a heuristic join never makes a phase certain; a
// retried Job's earlier pod is not shown running.
func checkPhaseMarkers(t *testing.T, s model.Story) {
	if len(s.Runs) != len(phasesWant) {
		t.Fatalf("runs = %d, want the failed attempt and its retry", len(s.Runs))
	}
	for i, run := range s.Runs {
		want := phasesWant[i]
		if len(run.Phases) != len(want) {
			t.Fatalf("run %s phases = %+v, want %d", run.Key, run.Phases, len(want))
		}
		for j, p := range run.Phases {
			if p.State != want[j].state || markKind(p.Start) != want[j].start || markKind(p.End) != want[j].end {
				t.Errorf("run %s phase %d = %s %q..%q, want %+v", run.Key, j, p.State, markKind(p.Start),
					markKind(p.End), want[j])
			}
			checkPhaseRecorded(t, run, p)
		}
		checkOneLivePod(t, run)
	}
	if dead, live := s.Runs[1].Phases[0], s.Runs[1].Phases[1]; dead.Pod == live.Pod {
		t.Errorf("the retry's agent spans are both pod %s's, want the Job's two pods", dead.Pod)
	}
	first := s.Runs[0].Phases[0]
	if first.Confidence != model.ConfidenceHeuristic || first.Pod == s.Runs[0].Pod {
		t.Errorf("the first pod's phase = %s from %s, want a heuristic join of a pod no run names", first.Confidence,
			first.Pod)
	}
	kelos := coverageOf(s, model.SourceKelos)
	if kelos == nil || !slices.Contains(kelos.Notes, model.PhaseMarkersNote) {
		t.Error("Kelos coverage does not say phase markers are best effort")
	}
}

// checkPhaseRecorded fails t unless each known bound of p is the time of
// an edge its pod recorded: no bound comes from the run, a pod or a gap.
func checkPhaseRecorded(t *testing.T, run model.Run, p model.Phase) {
	t.Helper()
	if p.Start == nil && p.End == nil {
		t.Errorf("run %s phase %s has no recorded edge", run.Key, p.Span)
	}
	if len(p.Evidence) == 0 || p.Pod == "" || p.PodUID == "" {
		t.Errorf("run %s phase %s: evidence %v from pod %q (uid %q)", run.Key, p.Span, p.Evidence, p.Pod, p.PodUID)
	}
	if p.Interval == nil {
		if p.State != model.PhaseInconsistent || p.Start == nil || p.End == nil {
			t.Errorf("run %s phase %s has no interval but is %s", run.Key, p.Span, p.State)
		}
		return
	}
	for _, b := range []struct {
		bound model.Bound
		mark  *model.PhaseMark
	}{{p.Interval.Start, p.Start}, {p.Interval.End, p.End}} {
		at, known := b.bound.Time()
		if known != (b.mark != nil) || known && !at.Equal(b.mark.At) {
			t.Errorf("run %s phase %s: bound %+v, recorded %+v", run.Key, p.Span, b.bound, b.mark)
		}
	}
	if p.Interval.End.State == model.BoundOpen && run.Interval.End.State != model.BoundOpen {
		t.Errorf("run %s phase %s is open, run ends %s", run.Key, p.Span, run.Interval.End.State)
	}
}

// checkOneLivePod fails t unless the open phases of run are all one pod's:
// a start alone from any other pod is interrupted.
func checkOneLivePod(t *testing.T, run model.Run) {
	t.Helper()
	live := ""
	for _, p := range run.Phases {
		if p.Interval == nil || p.Interval.End.State != model.BoundOpen {
			continue
		}
		if live != "" && p.Pod != live {
			t.Errorf("run %s has open phases from pods %s and %s", run.Key, live, p.Pod)
		}
		live = p.Pod
	}
}

func markKind(m *model.PhaseMark) model.MarkerKind {
	if m == nil {
		return ""
	}
	return m.Kind
}

func eventsForPR(s model.Story, kind model.EventKind, key model.PRKey) []model.Event {
	var out []model.Event
	for _, e := range eventsOfKind(s, kind) {
		if e.Related.PR != nil && *e.Related.PR == key {
			out = append(out, e)
		}
	}
	return out
}

func eventsOfKind(s model.Story, kind model.EventKind) []model.Event {
	var out []model.Event
	for _, e := range s.Events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func eventsByActorKind(s model.Story, kind model.EventKind, actor model.ActorKind) []model.Event {
	var out []model.Event
	for _, e := range eventsOfKind(s, kind) {
		if e.Actor.Kind == actor {
			out = append(out, e)
		}
	}
	return out
}

func onlyEvent(t *testing.T, s model.Story, kind model.EventKind) model.Event {
	t.Helper()
	events := eventsOfKind(s, kind)
	if len(events) != 1 {
		t.Fatalf("%s events = %d, want 1", kind, len(events))
	}
	return events[0]
}

func correlationsFrom(s model.Story, from model.Ref, to model.RefKind) []model.Correlation {
	var out []model.Correlation
	for _, c := range s.Correlations {
		if c.From == from && c.To.Kind == to {
			out = append(out, c)
		}
	}
	return out
}

func coverageOf(s model.Story, source model.Source) *model.Coverage {
	for i := range s.Coverage {
		if s.Coverage[i].Source == source {
			return &s.Coverage[i]
		}
	}
	return nil
}

func hasCoverage(s model.Story, source model.Source) bool {
	c := coverageOf(s, source)
	return c != nil && c.LastSuccessfulRead != nil
}

// RefsResolve fails t for each correlation of s that points outside it.
func RefsResolve(t *testing.T, s model.Story) {
	t.Helper()
	known := s.Refs(s.Root)
	for _, c := range s.Correlations {
		for _, ref := range []model.Ref{c.From, c.To} {
			if !known[ref] {
				t.Errorf("correlation %s: %s is not in the story", c, ref)
			}
		}
	}
}
