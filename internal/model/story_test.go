package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestBuildStoryRejectsInvalidRoot(t *testing.T) {
	task, page := testTaskKey(), testPageKey()
	tests := []struct {
		name string
		root Root
	}{
		{"empty", Root{}},
		{"both", Root{Task: &task, Page: &page}},
		{"invalid task", Root{Task: &TaskKey{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			story, err := BuildStory(tt.root, Records{})
			if err == nil {
				t.Fatalf("BuildStory() = %+v, want an error", story)
			}
		})
	}
}

func TestBuildStoryAcceptsEmptyRecords(t *testing.T) {
	story, err := BuildStory(testRoot(), Records{})
	if err != nil {
		t.Fatalf("BuildStory() error = %v", err)
	}
	if story.Root.Task == nil || *story.Root.Task != testTaskKey() {
		t.Errorf("Root = %+v, want task %v", story.Root, testTaskKey())
	}
	data, err := json.Marshal(story)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("null")) {
		t.Errorf("story JSON = %s, want every list as [], never null", data)
	}
}

// storyCase is a set of records and the error building a story from them
// must give; an empty want means they are accepted.
type storyCase = struct {
	name string
	recs Records
	want string
}

func runStoryCases(t *testing.T, tests []storyCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			story, err := BuildStory(testRoot(), tt.recs)
			checkErr(t, err, tt.want)
			if tt.want == "" && len(story.Events)+len(story.Runs) != len(tt.recs.Events)+len(tt.recs.Runs) {
				t.Errorf("story kept %d events and %d runs, want %d and %d",
					len(story.Events), len(story.Runs), len(tt.recs.Events), len(tt.recs.Runs))
			}
		})
	}
}

func TestBuildStoryRejectsDuplicates(t *testing.T) {
	otherInstance := testEvent("c-1")
	otherInstance.Instance = "vikunja-2.example.com"
	otherSource := testEvent("c-1")
	otherSource.Source = SourceWiki

	tests := []storyCase{
		{
			"duplicate events",
			Records{Events: []Event{testEvent("c-1"), testEvent("c-1")}},
			"vikunja/vikunja.example.com/c-1: duplicate record",
		},
		{
			"events differing only in kind",
			Records{Events: []Event{testEvent("c-1"), func() Event {
				e := testEvent("c-1")
				e.Kind = KindRequest
				return e
			}()}},
			"duplicate record",
		},
		{
			"events with different ids",
			Records{Events: []Event{testEvent("c-1"), testEvent("c-2")}},
			"",
		},
		{
			"events with the same id on another instance",
			Records{Events: []Event{testEvent("c-1"), otherInstance}},
			"",
		},
		{
			"events with the same id from another source",
			Records{Events: []Event{testEvent("c-1"), otherSource}},
			"",
		},
	}
	runStoryCases(t, tests)
}

func TestBuildStoryRejectsDuplicateRecords(t *testing.T) {
	tests := []storyCase{
		{
			"duplicate runs",
			Records{Runs: []Run{testRun("uid-1"), testRun("uid-1")}},
			"cluster-a/agents/uid/uid-1: duplicate record",
		},
		{
			"a retry with another uid and the same task name",
			Records{Runs: []Run{testRun("uid-1"), testRun("uid-2")}},
			"",
		},
		{
			"one pod keyed by pod and by uid",
			Records{Runs: []Run{withPod(testRun("uid-1"), testPod), podKeyedRun(testPod)}},
			"a run keyed by UID names the same pod",
		},
		{
			"a pod-keyed run and a uid-keyed run of another pod",
			Records{Runs: []Run{withPod(testRun("uid-1"), testPod), podKeyedRun("other-pod")}},
			"",
		},
		{
			"one correlation recorded from each end",
			Records{Correlations: []Correlation{testCorrelation(), reversed(testCorrelation())}},
			"duplicate record",
		},
		{
			"duplicate pull requests",
			Records{PullRequests: []PullRequest{testPullRequest(), testPullRequest()}},
			"duplicate record",
		},
		{
			"duplicate correlations",
			Records{Correlations: []Correlation{testCorrelation(), testCorrelation()}},
			"duplicate record",
		},
		{
			"duplicate coverage",
			Records{Coverage: []Coverage{testCoverage(), testCoverage()}},
			"duplicate record",
		},
	}
	runStoryCases(t, tests)
}

func TestBuildStoryKeepsRetryAsTwoRuns(t *testing.T) {
	first, retry := testRun("uid-1"), testRun("uid-2")
	if first.TaskName != retry.TaskName {
		t.Fatalf("task names %q and %q differ; the test needs a shared name", first.TaskName, retry.TaskName)
	}
	story, err := BuildStory(testRoot(), Records{Runs: []Run{retry, first}})
	if err != nil {
		t.Fatalf("BuildStory() error = %v", err)
	}
	var uids []string
	for _, r := range story.Runs {
		uids = append(uids, r.Key.UID)
	}
	if want := []string{"uid-1", "uid-2"}; !slices.Equal(uids, want) {
		t.Errorf("run uids = %v, want %v", uids, want)
	}
}

func TestBuildStoryReportsEveryInvalidRecord(t *testing.T) {
	badEvent1, badEvent2 := testEvent("bad-1"), testEvent("bad-2")
	badEvent1.ObservedAt = time.Time{}
	badEvent2.Kind = KindPRMerged
	badRun := testRun("bad-run")
	badRun.Role = "reviewer"
	badPR := testPullRequest()
	badPR.Branch = ""
	badCorrelation := testCorrelation()
	badCorrelation.Confidence = "certain"
	badCoverage := testCoverage()
	badCoverage.Instance = ""

	recs := Records{
		Events:       []Event{badEvent1, testEvent("ok"), badEvent2},
		Runs:         []Run{badRun},
		PullRequests: []PullRequest{badPR},
		Correlations: []Correlation{badCorrelation},
		Coverage:     []Coverage{badCoverage},
	}
	_, err := BuildStory(testRoot(), recs)
	if err == nil {
		t.Fatal("BuildStory() error = nil, want one")
	}
	for _, want := range []string{
		"event vikunja/vikunja.example.com/bad-1",
		"event vikunja/vikunja.example.com/bad-2",
		"run cluster-a/agents/uid/bad-run",
		"pull request github.example.com/example-org/example-repo/pull/12",
		"correlation run:cluster-a/agents/uid/uid-1",
		"coverage vikunja/",
	} {
		checkErr(t, err, want)
	}
}

func TestBuildStoryReportsInvalidAndDuplicateTogether(t *testing.T) {
	bad := testEvent("bad")
	bad.ID = ""
	recs := Records{
		Events: []Event{bad, testEvent("c-1"), testEvent("c-1")},
		Runs:   []Run{testRun("uid-1"), testRun("uid-1")},
	}
	_, err := BuildStory(testRoot(), recs)
	checkErr(t, err, "source, instance and id are required")
	checkErr(t, err, "vikunja/vikunja.example.com/c-1: duplicate record")
	checkErr(t, err, "cluster-a/agents/uid/uid-1: duplicate record")
}

// estRecords returns valid records whose every time is in testEST, whose
// lists are not in sorted order and whose nested lists are not either.
func estRecords() Records {
	return Records{
		Events:       estEvents(),
		Runs:         estRuns(),
		PullRequests: estPullRequests(),
		Correlations: estCorrelations(),
		Coverage:     estCoverage(),
	}
}

func estEvents() []Event {
	late, early, polled := testEvent("c-late"), testEvent("c-early"), testEvent("polled")
	late.SourceTime, late.ObservedAt = timePtr(atEST(50)), atEST(51)
	early.SourceTime, early.ObservedAt = timePtr(atEST(5)), atEST(6)
	polled.Kind, polled.SourceTime, polled.ObservedAt = KindTaskBucketObserved, nil, atEST(20)
	polled.Actor = Account{Kind: ActorUnknown}
	return []Event{late, polled, early}
}

func estRuns() []Run {
	newer, older := testRun("uid-2"), testRun("uid-1")
	newer.Interval = Interval{Start: KnownAt(atEST(40)), End: KnownAt(atEST(60))}
	older.Interval = Interval{Start: KnownAt(atEST(0)), End: KnownAt(atEST(30))}
	newer.Created = timePtr(atEST(35))
	newer.Usage = &Usage{CostUSD: "0.42", InputTokens: int64Ptr(1200)}
	newer.Evidence = []Evidence{
		{Source: SourceKelos, Instance: testCluster, Ref: "tasks/z"},
		{Source: SourceKelos, Instance: testCluster, Ref: "tasks/a"},
	}
	return []Run{newer, older}
}

func estPullRequests() []PullRequest {
	pr1, pr2 := testPullRequest(), testPullRequest()
	pr1.Key.Number, pr2.Key.Number = 9, 3
	pr1.Checks = []CheckRun{
		{
			ID: 2, SHA: "bbb", Name: "build", Conclusion: CheckSuccess,
			Interval: Interval{Start: KnownAt(atEST(20)), End: KnownAt(atEST(25))},
		},
		{
			ID: 1, SHA: "aaa", Name: "build", Conclusion: CheckFailure,
			Interval: Interval{Start: KnownAt(atEST(10)), End: KnownAt(atEST(15))},
		},
	}
	pr2.Checks[0].Interval = Interval{Start: KnownAt(atEST(1)), End: KnownAt(atEST(4))}
	return []PullRequest{pr1, pr2}
}

func estCorrelations() []Correlation {
	c1, c2 := testCorrelation(), testCorrelation()
	c1.From = testRunKey("uid-2").Ref()
	c1.Evidence = []Evidence{
		{Source: SourceKelos, Instance: testCluster, Ref: "b"},
		{Source: SourceKelos, Instance: testCluster, Ref: "a"},
	}
	return []Correlation{c1, c2}
}

func estCoverage() []Coverage {
	wiki, vikunja := testCoverage(), testCoverage()
	wiki.Source, wiki.Instance = SourceWiki, testWikiHost
	wiki.LastSuccessfulRead = timePtr(atEST(120))
	vikunja.LastSuccessfulRead = timePtr(atEST(90))
	vikunja.Gaps = []Gap{
		{Start: atEST(70), End: atEST(75), Reason: "later"},
		{Start: atEST(30), End: atEST(35), Reason: "earlier"},
	}
	return []Coverage{wiki, vikunja}
}

func TestBuildStoryConvertsEveryTimeToUTC(t *testing.T) {
	story, err := BuildStory(testRoot(), estRecords())
	if err != nil {
		t.Fatalf("BuildStory() error = %v", err)
	}
	check := func(what string, got time.Time) {
		t.Helper()
		if got.Location() != time.UTC {
			t.Errorf("%s location = %v, want UTC", what, got.Location())
		}
	}
	checkBound := func(what string, b Bound) {
		t.Helper()
		if b.State == BoundKnown {
			check(what, *b.At)
		}
	}
	for _, e := range story.Events {
		if e.SourceTime != nil {
			check("event "+e.ID+" SourceTime", *e.SourceTime)
		}
		check("event "+e.ID+" ObservedAt", e.ObservedAt)
	}
	for _, r := range story.Runs {
		if r.Created != nil {
			check("run "+r.Key.UID+" Created", *r.Created)
		}
		checkBound("run "+r.Key.UID+" start", r.Interval.Start)
		checkBound("run "+r.Key.UID+" end", r.Interval.End)
	}
	for _, p := range story.PullRequests {
		for _, c := range p.Checks {
			checkBound("check start", c.Interval.Start)
			checkBound("check end", c.Interval.End)
		}
	}
	for _, c := range story.Coverage {
		if c.LastSuccessfulRead != nil {
			check("coverage "+c.Key()+" LastSuccessfulRead", *c.LastSuccessfulRead)
		}
		for _, g := range c.Gaps {
			check("gap "+g.Reason+" start", g.Start)
			check("gap "+g.Reason+" end", g.End)
		}
	}
}

func TestBuildStoryKeepsInstants(t *testing.T) {
	story, err := BuildStory(testRoot(), estRecords())
	if err != nil {
		t.Fatalf("BuildStory() error = %v", err)
	}
	if got, want := story.Events[0].Time(), at(5); !got.Equal(want) {
		t.Errorf("first event time = %v, want %v", got, want)
	}
	if got, want := *story.Runs[0].Interval.Start.At, at(0); !got.Equal(want) {
		t.Errorf("first run start = %v, want %v", got, want)
	}
	if got, want := *story.Coverage[0].LastSuccessfulRead, at(90); !got.Equal(want) {
		t.Errorf("vikunja last read = %v, want %v", got, want)
	}
}

func TestBuildStorySortsEveryList(t *testing.T) {
	story, err := BuildStory(testRoot(), estRecords())
	if err != nil {
		t.Fatalf("BuildStory() error = %v", err)
	}
	var ids []string
	for _, e := range story.Events {
		ids = append(ids, e.ID)
	}
	if want := []string{"c-early", "polled", "c-late"}; !slices.Equal(ids, want) {
		t.Errorf("event ids = %v, want %v", ids, want)
	}
	if got := []string{story.Runs[0].Key.UID, story.Runs[1].Key.UID}; !slices.Equal(got, []string{"uid-1", "uid-2"}) {
		t.Errorf("run uids = %v, want [uid-1 uid-2]", got)
	}
	if got := []int{story.PullRequests[0].Key.Number, story.PullRequests[1].Key.Number}; got[0] != 3 || got[1] != 9 {
		t.Errorf("pull request numbers = %v, want [3 9]", got)
	}
	if got := story.PullRequests[1].Checks[0].ID; got != 1 {
		t.Errorf("first check id = %d, want 1", got)
	}
	if got := story.Runs[1].Evidence[0].Ref; got != "tasks/a" {
		t.Errorf("first evidence ref = %q, want tasks/a", got)
	}
	if got := story.Correlations[1].Evidence[0].Ref; got != "a" {
		t.Errorf("first correlation evidence ref = %q, want a", got)
	}
	if got := story.Coverage[0].Source; got != SourceVikunja {
		t.Errorf("first coverage source = %q, want %q", got, SourceVikunja)
	}
	if got := story.Coverage[0].Gaps[0].Reason; got != "earlier" {
		t.Errorf("first gap = %q, want earlier", got)
	}
}

func TestBuildStoryDoesNotModifyInput(t *testing.T) {
	got, want := estRecords(), estRecords()
	story, err := BuildStory(testRoot(), got)
	if err != nil {
		t.Fatalf("BuildStory() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildStory modified its input:\n got %+v\nwant %+v", got, want)
	}
	// Spot-check what DeepEqual would also catch, to name the failure.
	if loc := got.Events[0].ObservedAt.Location(); loc != testEST {
		t.Errorf("input event time location = %v, want %v", loc, testEST)
	}
	if loc := got.Runs[0].Interval.Start.At.Location(); loc != testEST {
		t.Errorf("input run start location = %v, want %v", loc, testEST)
	}
	if id := got.Events[0].ID; id != "c-late" {
		t.Errorf("input first event = %q, want c-late (input order)", id)
	}
	if loc := got.Runs[0].Created.Location(); loc != testEST {
		t.Errorf("input run creation location = %v, want %v", loc, testEST)
	}
	if ref := got.Runs[0].Evidence[0].Ref; ref != "tasks/z" {
		t.Errorf("input run evidence first ref = %q, want tasks/z", ref)
	}
	if id := got.PullRequests[0].Checks[0].ID; id != 2 {
		t.Errorf("input first check id = %d, want 2", id)
	}
	if reason := got.Coverage[1].Gaps[0].Reason; reason != "later" {
		t.Errorf("input first gap = %q, want later", reason)
	}

	// The story must not share its lists with the input either.
	story.Events[0].ID = "mutated"
	story.Runs[1].Evidence[0].Ref = "mutated"
	*story.Runs[1].Usage.InputTokens = -1
	story.Runs[1].Usage.CostUSD = "mutated"
	story.PullRequests[0].Checks[0].ID = -1
	story.Correlations[1].Evidence[0].Ref = "mutated"
	story.Coverage[0].Gaps = append(story.Coverage[0].Gaps[:0], Gap{Reason: "mutated"})
	if !reflect.DeepEqual(got, want) {
		t.Error("changing the story changed the input records")
	}
}

func TestBuildStoryIsRepeatable(t *testing.T) {
	recs := estRecords()
	first, err := BuildStory(testRoot(), recs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildStory(testRoot(), recs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("building twice from the same records gave different stories")
	}
}

// testPod is the pod of the run the pod-keying tests share.
const testPod = "task-42-abcde"

func withPod(r Run, pod string) Run {
	r.Pod = pod
	return r
}

// podKeyedRun returns a valid run known only from an announcement naming
// pod.
func podKeyedRun(pod string) Run {
	r := testRun("")
	r.Key.Pod = pod
	r.Pod = pod
	r.Interval = Interval{Start: UnknownBound(), End: UnknownBound()}
	r.Outcome = OutcomeUnknown
	return r
}

func reversed(c Correlation) Correlation {
	c.From, c.To = c.To, c.From
	return c
}
