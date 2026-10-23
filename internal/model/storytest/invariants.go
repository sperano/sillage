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

	"github.com/sperano/sillage/internal/model"
)

// invariants asserts, for each fixture story, the property it exists to
// protect. Every directory under testdata/fixtures/stories needs an entry.
// The collector's tests assert the same properties of the stories it
// assembles from source responses.
var invariants = map[string]func(*testing.T, model.Story){
	"discuss-no-pr":         checkDiscussNoPR,
	"implementation-merged": checkImplementationMerged,
	"wiki-rooted-request":   checkWikiRootedRequest,
	"failed-run-retry":      checkFailedRunRetry,
	"announced-run-only":    checkAnnouncedRunOnly,
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
// coding phase; the deliverable is a page and a reply.
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
	if len(eventsOfKind(s, model.KindPageCreated)) != 1 {
		t.Error("the deliverable page's creation is missing")
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

// checkWikiRootedRequest: a story can start from a page with no task; the
// live version is an edit of its own; an ambiguous edit keeps every
// candidate run.
func checkWikiRootedRequest(t *testing.T, s model.Story) {
	if s.Root.Task != nil || s.Root.Page == nil {
		t.Fatalf("root = %+v, want a page and no task", s.Root)
	}
	for _, e := range s.Events {
		if e.Related.Task != nil {
			t.Errorf("event %s names a task", e.Key())
		}
	}
	order := []model.EventKind{model.KindRequest, model.KindRunAnnounced, model.KindPageEdited, model.KindComment}
	if got := kindsAfter(s, model.KindRequest); !isSubsequence(order, got) {
		t.Errorf("kinds from the request on = %v, want %v in order", got, order)
	}

	edits := eventsOfKind(s, model.KindPageEdited)
	if len(edits) != 2 {
		t.Fatalf("edits = %d, want 2", len(edits))
	}
	live := edits[1]
	if live.IDKind != model.IDFingerprint || live.Details["version"] != "live" {
		t.Errorf("last edit %s is not the live version with a fingerprint id", live.Key())
	}

	candidates := correlationsFrom(s, live.Ref(), model.RefRun)
	if len(candidates) != 2 {
		t.Fatalf("runs correlated with the live edit = %d, want 2", len(candidates))
	}
	for _, c := range candidates {
		if !c.Ambiguous || c.Confidence != model.ConfidenceHeuristic {
			t.Errorf("correlation %s is not an ambiguous heuristic", c)
		}
		if findRun(s, c.To) == nil {
			t.Errorf("candidate %s is not a run of the story", c.To)
		}
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
	if len(eventsByActorKind(s, model.KindComment, model.ActorAgent)) == 0 {
		t.Error("the fixture lost the agent's later comment it exists to resist")
	}
	announced := onlyEvent(t, s, model.KindRunAnnounced)
	kelos := coverageOf(s, model.SourceKelos)
	if kelos == nil || kelos.Known(announced.Time()) {
		t.Error("Kelos coverage does not mark the run's period unknown")
	}
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

// kindsAfter returns the kinds of the events from the first of kind on.
func kindsAfter(s model.Story, kind model.EventKind) []model.EventKind {
	var out []model.EventKind
	for _, e := range s.Events {
		if len(out) > 0 || e.Kind == kind {
			out = append(out, e.Kind)
		}
	}
	return out
}

// isSubsequence reports whether want appears in got in order.
func isSubsequence(want, got []model.EventKind) bool {
	i := 0
	for _, k := range got {
		if i < len(want) && k == want[i] {
			i++
		}
	}
	return i == len(want)
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

func findRun(s model.Story, ref model.Ref) *model.Run {
	for i := range s.Runs {
		if s.Runs[i].Key.Ref() == ref {
			return &s.Runs[i]
		}
	}
	return nil
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

// Refs returns every record a correlation of s may point to: its root,
// its events and the tasks and pages they name, its runs and its pull
// requests.
func Refs(s model.Story) map[model.Ref]bool {
	known := make(map[model.Ref]bool)
	addRelated := func(r model.Related) {
		if r.Task != nil {
			known[r.Task.Ref()] = true
		}
		if r.Page != nil {
			known[r.Page.Ref()] = true
		}
	}
	addRelated(model.Related{Task: s.Root.Task, Page: s.Root.Page})
	for _, e := range s.Events {
		known[e.Ref()] = true
		addRelated(e.Related)
	}
	for _, r := range s.Runs {
		known[r.Key.Ref()] = true
	}
	for _, p := range s.PullRequests {
		known[p.Key.Ref()] = true
	}
	return known
}

// RefsResolve fails t for each correlation of s that points outside it.
func RefsResolve(t *testing.T, s model.Story) {
	t.Helper()
	known := Refs(s)
	for _, c := range s.Correlations {
		for _, ref := range []model.Ref{c.From, c.To} {
			if !known[ref] {
				t.Errorf("correlation %s: %s is not in the story", c, ref)
			}
		}
	}
}
