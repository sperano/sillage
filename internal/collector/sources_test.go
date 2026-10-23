package collector

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/kelos"
	"github.com/sperano/sillage/internal/source/vikunja"
	"github.com/sperano/sillage/internal/source/wiki"
)

// errDown is every failing fake's error.
var errDown = errors.New("fabricated outage")

type failingVikunja struct{}

func (failingVikunja) Fetch(context.Context, int64) (vikunja.Snapshot, error) {
	return vikunja.Snapshot{}, errDown
}

type failingKelos struct{}

func (failingKelos) Read(context.Context) (kelos.Snapshot, error) { return kelos.Snapshot{}, errDown }

type failingGitHub struct{}

func (failingGitHub) FetchBranch(context.Context, github.Repository, github.Branch) ([]github.Snapshot, error) {
	return nil, errDown
}

type failingWiki struct{}

func (failingWiki) Fetch(context.Context, int64) (wiki.Snapshot, error) {
	return wiki.Snapshot{}, errDown
}

func (failingWiki) Pages(context.Context) ([]wiki.Page, error) { return nil, errDown }

// fixedVikunja, fixedKelos and fixedGitHub answer with what they hold.
type fixedVikunja struct{ snap vikunja.Snapshot }

func (f fixedVikunja) Fetch(context.Context, int64) (vikunja.Snapshot, error) { return f.snap, nil }

type fixedKelos struct{ snap kelos.Snapshot }

func (f fixedKelos) Read(context.Context) (kelos.Snapshot, error) { return f.snap, nil }

type fixedGitHub struct{ snaps []github.Snapshot }

func (f fixedGitHub) FetchBranch(context.Context, github.Repository, github.Branch) ([]github.Snapshot, error) {
	return f.snaps, nil
}

// at is a fabricated time on 2026-03-05, minutes after noon UTC.
func at(minutes int) time.Time {
	return time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

func metaTime(minutes int) *metav1.Time {
	t := metav1.NewTime(at(minutes))
	return &t
}

// record is a TaskRecord of a finished attempt, its Task gone.
func record(name, uid string, labels map[string]string, start, end int) kelos.TaskRecord {
	return kelos.TaskRecord{
		ObjectMeta: metav1.ObjectMeta{Name: uid, Labels: labels},
		Spec: kelos.TaskRecordSpec{
			TaskRef: kelos.TaskReference{Name: name, UID: types.UID(uid)}, Phase: kelos.PhaseSucceeded,
			StartTime: metaTime(start), CompletionTime: metaTime(end),
		},
	}
}

// task is a finished Task working on branch.
func task(name, uid, branch string, labels map[string]string, start, end int) kelos.Task {
	return kelos.Task{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid), Labels: labels,
			CreationTimestamp: *metaTime(start)},
		Spec: kelos.TaskSpec{Branch: branch},
		Status: kelos.TaskStatus{Phase: kelos.PhaseSucceeded, PodName: name + "-pod00",
			StartTime: metaTime(start), CompletionTime: metaTime(end)},
	}
}

// taskLabels are the spawners' labels of an implementation of task id.
func taskLabels(id string) map[string]string {
	return map[string]string{
		testVikunjaHost + "/project": testWorkspace, testVikunjaHost + "/variant": testAgent,
		testVikunjaHost + "/task": id,
	}
}

// announcedTask is a task whose service account comments announce one
// run each of Task name, pod-less when pod is empty.
func announcedTask(id int64, name string, pods map[int]string) vikunja.Snapshot {
	s := vikunja.Snapshot{
		Task:       vikunja.Task{ID: id, Created: at(0), CreatedBy: &vikunja.User{ID: 7, Username: "owner"}},
		ObservedAt: testNow,
	}
	for minute, pod := range pods {
		body := "<p>Agent <code>" + testAgent + "</code> has started working on this task: Kelos Task <code>" + name +
			"</code>"
		if pod != "" {
			body += ", pod <code>" + pod + "</code>"
		}
		s.Comments = append(s.Comments, vikunja.Comment{
			ID: int64(9000 + minute), Body: body + ".</p>", Created: at(minute), Updated: at(minute),
			Author: vikunja.User{ID: 23, Username: testServiceAccount},
		})
	}
	return s
}

// collectWith assembles root's story from src.
func collectWith(t *testing.T, cfg Config, src Sources, root model.Root, repos ...github.Repository) Result {
	t.Helper()
	c, err := New(cfg, src)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	res, err := c.Collect(t.Context(), root, repos...)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return res
}

// emptySources are sources with nothing to read.
func emptySources() Sources {
	return Sources{
		Vikunja: fixedVikunja{announcedTask(1, "", nil)}, Kelos: fixedKelos{}, GitHub: fixedGitHub{}, Wiki: failingWiki{},
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
