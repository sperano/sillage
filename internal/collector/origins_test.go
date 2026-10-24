package collector

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/wiki"
)

// fixedWiki serves one page.
type fixedWiki struct{ snap wiki.Snapshot }

func (w fixedWiki) Fetch(context.Context, int64) (wiki.Snapshot, error) { return w.snap, nil }

func (w fixedWiki) Pages(context.Context) ([]wiki.Page, error) { return []wiki.Page{w.snap.Page}, nil }

// fakeMemory is an OriginMemory in memory.
type fakeMemory struct {
	kept      map[model.RunKey][]byte
	keeps     []map[model.RunKey][]byte
	recalled  [][]model.RunKey
	keepErr   error
	recallErr error
}

func newFakeMemory() *fakeMemory { return &fakeMemory{kept: make(map[model.RunKey][]byte)} }

func (m *fakeMemory) KeepOrigins(_ context.Context, origins map[model.RunKey][]byte) error {
	if m.keepErr != nil {
		return m.keepErr
	}
	m.keeps = append(m.keeps, maps.Clone(origins))
	for k, v := range origins {
		if _, ok := m.kept[k]; !ok {
			m.kept[k] = v
		}
	}
	return nil
}

func (m *fakeMemory) RecallOrigins(_ context.Context, runs []model.RunKey) (map[model.RunKey][]byte, error) {
	if m.recallErr != nil {
		return nil, m.recallErr
	}
	m.recalled = append(m.recalled, slices.Clone(runs))
	out := make(map[model.RunKey][]byte)
	for _, k := range runs {
		if v, ok := m.kept[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// remember keeps origin as the memory's for run uid, read at seen.
func (m *fakeMemory) remember(t *testing.T, uid string, seen time.Time, annotations map[string]string) {
	t.Helper()
	body, err := json.Marshal(kelos.Origin{Task: "tasks/agents/gone", Annotations: annotations, SeenAt: seen})
	if err != nil {
		t.Fatal(err)
	}
	m.kept[runKeyOf(uid)] = body
}

func runKeyOf(uid string) model.RunKey {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}
}

// annotate sets annotations, by name after the label domain, on t.
func annotate(t kelos.Task, values map[string]string) kelos.Task {
	t.Annotations = make(map[string]string, len(values))
	for k, v := range values {
		t.Annotations[testVikunjaHost+"/"+k] = v
	}
	return t
}

// prOrigin is the Origin of a PR follow-up asked for by GitHub comment
// 9101 on pull request number.
func prOrigin(number string) map[string]string {
	return map[string]string{
		kelos.AnnotationSource: kelos.OriginGitHub, kelos.AnnotationSourceInstance: testGitHubHost,
		kelos.AnnotationRepository: testRepo.String(), kelos.AnnotationPR: number,
		kelos.AnnotationEvent: kelos.EventIssueComment, kelos.AnnotationComment: "9101",
	}
}

// collectWithMemory assembles task 104's story through m, with the pull
// requests of its branch.
func collectWithMemory(t *testing.T, m OriginMemory, snap kelos.Snapshot, prs ...github.Snapshot) Result {
	t.Helper()
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	src.Kelos = fixedKelos{snap}
	src.GitHub = fixedGitHub{prs}
	c, err := New(testConfig(), src, WithOriginMemory(m))
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	res, err := c.Collect(t.Context(), taskRoot(104), testRepo)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// prWithComment is pull request number of task 104's branch, with
// conversation comment 9101.
func prWithComment(number int) github.Snapshot {
	pr := pullRequest(number, taskBranch(104))
	pr.Comments = []github.Comment{{ID: 9101, User: &github.User{Login: "owner-gh", Type: "User"},
		Body: "please fix", CreatedAt: at(1)}}
	return pr
}

// A Task with annotations and no labels is joined to the pull request
// and the comment its Origin names, as identifiers, and by nothing else:
// its name names another pull request.
func TestAnnotatedTaskWithoutLabels(t *testing.T) {
	follow := annotate(task("pr-followup-widgets-8-c1", "uid-annotated", "", nil, 1, 2), prOrigin("7"))
	s := collectPRs(t, kelos.Snapshot{Tasks: []kelos.Task{follow}}, prWithComment(7), pullRequest(8, taskBranch(104)))
	run := runRef("uid-annotated")
	c, ok := linkBetween(s, run, prRef(7))
	if !ok || c.Confidence != model.ConfidenceIdentifier || !strings.Contains(c.Evidence[0].Note, "/pr=7") {
		t.Errorf("run -> PR 7 = %+v, %v; want an identifier from the pr annotation", c, ok)
	}
	if c, ok := linkBetween(s, run, prRef(8)); ok {
		t.Errorf("run joined to PR 8 by its name: %+v", c)
	}
	comment := model.Event{Source: model.SourceGitHub, Instance: testGitHubHost, ID: github.CommentEventID(9101)}
	if c, ok := linkBetween(s, run, comment.Ref()); !ok || c.Confidence != model.ConfidenceIdentifier {
		t.Errorf("run -> request comment = %+v, %v; want an identifier", c, ok)
	}
}

// A run read from its TaskRecord alone, whose Origin was kept when its
// Task was read, is joined by that Origin, not by its name.
func TestRecordOnlyRunJoinedByAKeptOrigin(t *testing.T) {
	m := newFakeMemory()
	seen := at(-60)
	m.remember(t, "uid-gone", seen, prOrigin("7"))
	followUp := map[string]string{testVikunjaHost + "/workspace": testWorkspace, testVikunjaHost + "/role": "pr-followup"}
	snap := kelos.Snapshot{Records: []kelos.TaskRecord{record("pr-followup-widgets-8-c3", "uid-gone", followUp, 1, 2)}}
	res := collectWithMemory(t, m, snap, prWithComment(7), pullRequest(8, taskBranch(104)))
	for _, f := range res.Failures {
		t.Errorf("failed read: %v", f)
	}
	c, ok := linkBetween(res.Story, runRef("uid-gone"), prRef(7))
	if !ok || c.Confidence != model.ConfidenceIdentifier ||
		!strings.Contains(c.Evidence[0].Note, "read from the Task at "+seen.Format(time.RFC3339)) {
		t.Errorf("run -> PR 7 = %+v, %v; want an identifier from the kept origin", c, ok)
	}
	if c, ok := linkBetween(res.Story, runRef("uid-gone"), prRef(8)); ok {
		t.Errorf("run joined to PR 8 by its name: %+v", c)
	}
	if want := [][]model.RunKey{{runKeyOf("uid-gone")}}; !slices.EqualFunc(m.recalled, want, slices.Equal) {
		t.Errorf("recalled %v, want %v", m.recalled, want)
	}
}

// Every read keeps every listed Task's Origin; the memory keeps the
// first.
func TestEveryReadKeepsTheListedOrigins(t *testing.T) {
	m := newFakeMemory()
	annotated := annotate(task("pr-followup-widgets-7-c1", "uid-a", "", nil, 1, 2), prOrigin("7"))
	plain := task("vikunja-widgets-agent-opus-104", "uid-plain", taskBranch(104), taskLabels("104"), 1, 2)
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	src.Kelos = fixedKelos{kelos.Snapshot{Tasks: []kelos.Task{annotated, plain}, ObservedAt: at(3)}}
	c, err := New(testConfig(), src, WithOriginMemory(m))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := c.Collect(t.Context(), taskRoot(104)); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.keeps) != 2 || len(m.keeps[0]) != 1 || len(m.keeps[1]) != 1 {
		t.Fatalf("keeps = %v, want each read to keep the annotated Task's origin only", m.keeps)
	}
	var o kelos.Origin
	if err := json.Unmarshal(m.kept[runKeyOf("uid-a")], &o); err != nil {
		t.Fatal(err)
	}
	if o.Task != "tasks/agents/pr-followup-widgets-7-c1" || o.Value(kelos.AnnotationPR) != "7" || !o.SeenAt.Equal(at(3)) {
		t.Errorf("kept origin = %+v, want the Task's, seen at the read", o)
	}
	if len(m.recalled) != 0 {
		t.Errorf("recalled %v, but every run was read with its Task", m.recalled)
	}
}

// A nil memory keeps nothing and fails nothing.
func TestNilOriginMemory(t *testing.T) {
	src := emptySources()
	src.Kelos = fixedKelos{kelos.Snapshot{Records: []kelos.TaskRecord{record("pr-followup-widgets-7-c2", "uid-gone", nil, 1, 2)}}}
	c, err := New(testConfig(), src, WithOriginMemory(nil))
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Collect(t.Context(), taskRoot(1))
	if err != nil || len(res.Failures) != 0 {
		t.Errorf("Collect = %v, %v; want no failure", res.Failures, err)
	}
}

// Failing to keep or recall origins fails the Kelos read: the story would
// otherwise join the runs by heuristics a later read contradicts.
func TestOriginMemoryFailureFailsTheKelosRead(t *testing.T) {
	errDown := errors.New("database down")
	annotated := annotate(task("pr-followup-widgets-7-c1", "uid-a", "", nil, 1, 2), prOrigin("7"))
	gone := record("pr-followup-widgets-7-c2", "uid-gone", nil, 1, 2)
	for name, m := range map[string]*fakeMemory{
		"keep":   {kept: map[model.RunKey][]byte{}, keepErr: errDown},
		"recall": {kept: map[model.RunKey][]byte{}, recallErr: errDown},
	} {
		res := collectWithMemory(t, m, kelos.Snapshot{Tasks: []kelos.Task{annotated}, Records: []kelos.TaskRecord{gone}})
		if len(res.Failures) != 1 || res.Failures[0].Source != model.SourceKelos || !errors.Is(res.Failures[0], errDown) {
			t.Errorf("%s: failures = %v, want the Kelos read failed", name, res.Failures)
		}
		if len(res.Story.Runs) != 0 {
			t.Errorf("%s: runs = %v, want none from a failed read", name, res.Story.Runs)
		}
	}
}

// A run's Origin names the repository to read: no workspace mapping is
// needed, and none is noted missing.
func TestRepositoryFromTheOrigin(t *testing.T) {
	cfg := testConfig()
	cfg.Workspaces = nil
	values := map[string]string{
		kelos.AnnotationSource: kelos.OriginVikunja, kelos.AnnotationSourceInstance: testVikunjaHost,
		kelos.AnnotationRepository: testRepo.String(),
	}
	impl := annotate(task("vikunja-widgets-agent-opus-104", "uid-impl", taskBranch(104), taskLabels("104"), 1, 2), values)
	src := emptySources()
	src.Vikunja = fixedVikunja{announcedTask(104, "", nil)}
	src.Kelos = fixedKelos{kelos.Snapshot{Tasks: []kelos.Task{impl}}}
	src.GitHub = fixedGitHub{[]github.Snapshot{pullRequest(7, taskBranch(104))}}
	s := collectWith(t, cfg, src, taskRoot(104)).Story
	if len(s.PullRequests) != 1 {
		t.Errorf("pull requests = %v, want the branch's, read in the origin's repository", s.PullRequests)
	}
	if cov := coverageOf(t, s, model.SourceGitHub); hasNote(cov, "no repository") {
		t.Errorf("github coverage notes %v, want no missing repository", cov.Notes)
	}
}

// A merge Task, which has no spec.branch, is joined to the task by its
// branch annotation.
func TestMergeTaskJoinedByItsBranchAnnotation(t *testing.T) {
	merge := annotate(task("vikunja-merge-widgets-7-true", "uid-merge", "", nil, 1, 2), map[string]string{
		kelos.AnnotationSource: kelos.OriginGitHub, kelos.AnnotationSourceInstance: testGitHubHost,
		kelos.AnnotationRepository: testRepo.String(), kelos.AnnotationPR: "7",
		kelos.AnnotationBranch: taskBranch(104),
	})
	s := collectPRs(t, kelos.Snapshot{Tasks: []kelos.Task{merge}}, pullRequest(7, taskBranch(104)))
	c, ok := linkBetween(s, runRef("uid-merge"), taskRoot(104).Task.Ref())
	if !ok || c.Confidence != model.ConfidenceIdentifier ||
		!strings.Contains(c.Evidence[0].Note, "metadata.annotations["+testVikunjaHost+"/branch] "+taskBranch(104)) {
		t.Errorf("merge run -> task = %+v, %v; want an identifier from the branch annotation", c, ok)
	}
	if _, ok := linkBetween(s, runRef("uid-merge"), prRef(7)); !ok {
		t.Error("merge run not joined to PR 7")
	}
}

// A wiki follow-up is joined to the page and the comment its Origin
// names, not to those its name names.
func TestWikiFollowUpJoinedByItsOrigin(t *testing.T) {
	values := map[string]string{
		kelos.AnnotationSource: kelos.OriginWiki, kelos.AnnotationSourceInstance: testWikiHost,
		kelos.AnnotationWikiPage: "57", kelos.AnnotationWikiLocale: "en", kelos.AnnotationComment: "801",
	}
	run := annotate(task("wiki-fu-widgets-58-c802", "uid-wiki", "", nil, 1, 2), values)
	src := emptySources()
	src.Kelos = fixedKelos{kelos.Snapshot{Tasks: []kelos.Task{run}}}
	src.Wiki = fixedWiki{wiki.Snapshot{
		Instance: testWikiHost, ObservedAt: testNow,
		Page: wiki.Page{ID: 57, Path: "widgets/notes", Locale: "en", CreatedAt: at(0), UpdatedAt: at(0),
			AuthorID: 7, AuthorName: "Robin Owner", CreatorID: 7, CreatorName: "Robin Owner"},
		Comments: []wiki.Comment{{ID: 801, Render: "<p>please</p>", AuthorID: 7, AuthorName: "Robin Owner", CreatedAt: at(1)}},
	}}
	s := collectWith(t, testConfig(), src, pageRoot(57)).Story
	c, ok := linkBetween(s, runRef("uid-wiki"), pageRoot(57).Page.Ref())
	if !ok || c.Confidence != model.ConfidenceIdentifier || !strings.Contains(c.Evidence[0].Note, "/wiki-page=57") {
		t.Errorf("run -> page 57 = %+v, %v; want an identifier from the annotation", c, ok)
	}
	request := model.Event{Source: model.SourceWiki, Instance: testWikiHost, ID: commentObject(801)}
	if c, ok := linkBetween(s, runRef("uid-wiki"), request.Ref()); !ok || c.Confidence != model.ConfidenceIdentifier {
		t.Errorf("run -> comment 801 = %+v, %v; want an identifier from the comment annotation", c, ok)
	}
	for _, c := range s.Correlations {
		if c.From == runRef("uid-wiki") && strings.Contains(c.To.ID, "802") {
			t.Errorf("run joined to the comment its name names: %+v", c)
		}
	}
}

// An Origin naming another host than the configured instance joins
// nothing, so it does not keep a record-only run out of its stories: the
// label, branch and name joins still apply.
func TestOriginOnAnotherHostKeepsTheOtherJoins(t *testing.T) {
	m := newFakeMemory()
	m.remember(t, "uid-labelled", at(-60), map[string]string{
		kelos.AnnotationSource: kelos.OriginVikunja, kelos.AnnotationSourceInstance: "tasks.example.com",
	})
	other := prOrigin("7")
	other[kelos.AnnotationSourceInstance] = "github.example.org"
	m.remember(t, "uid-follow", at(-60), other)
	followUp := map[string]string{testVikunjaHost + "/workspace": testWorkspace, testVikunjaHost + "/role": "pr-followup"}
	snap := kelos.Snapshot{Records: []kelos.TaskRecord{
		record("vikunja-widgets-agent-opus-104", "uid-labelled", taskLabels("104"), 1, 2),
		record("pr-followup-widgets-7-c3", "uid-follow", followUp, 1, 2),
	}}
	res := collectWithMemory(t, m, snap, pullRequest(7, taskBranch(104)))
	if c, ok := linkBetween(res.Story, runRef("uid-labelled"), taskRoot(104).Task.Ref()); !ok ||
		c.Confidence != model.ConfidenceIdentifier || len(c.Evidence) != 1 {
		t.Errorf("labelled run -> task = %+v, %v; want the label's identifier alone", c, ok)
	}
	if c, ok := linkBetween(res.Story, runRef("uid-follow"), prRef(7)); !ok || c.Confidence != model.ConfidenceHeuristic {
		t.Errorf("follow-up -> PR 7 = %+v, %v; want the name's heuristic", c, ok)
	}
}

// A wiki follow-up read from its TaskRecord alone, whose kept Origin names
// the page on another host, is still joined by its name.
func TestWikiFollowUpOnAnotherHostJoinedByName(t *testing.T) {
	m := newFakeMemory()
	m.remember(t, "uid-wiki", at(-60), map[string]string{
		kelos.AnnotationSource: kelos.OriginWiki, kelos.AnnotationSourceInstance: "docs.example.com",
		kelos.AnnotationWikiPage: "57", kelos.AnnotationWikiLocale: "en", kelos.AnnotationComment: "801",
	})
	src := emptySources()
	src.Kelos = fixedKelos{kelos.Snapshot{Records: []kelos.TaskRecord{record("wiki-fu-widgets-57-c801", "uid-wiki", nil, 1, 2)}}}
	src.Wiki = fixedWiki{wiki.Snapshot{
		Instance: testWikiHost, ObservedAt: testNow,
		Page: wiki.Page{ID: 57, Path: "widgets/notes", Locale: "en", CreatedAt: at(0), UpdatedAt: at(0),
			AuthorID: 7, AuthorName: "Robin Owner", CreatorID: 7, CreatorName: "Robin Owner"},
	}}
	c, err := New(testConfig(), src, WithOriginMemory(m))
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	res, err := c.Collect(t.Context(), pageRoot(57))
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := linkBetween(res.Story, runRef("uid-wiki"), pageRoot(57).Page.Ref()); !ok ||
		!strings.Contains(l.Evidence[0].Note, "Task name names page 57") {
		t.Errorf("run -> page 57 = %+v, %v; want the name's join", l, ok)
	}
}
