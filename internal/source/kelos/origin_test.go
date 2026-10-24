package kelos

import (
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/sperano/sillage/internal/model"
)

// annotated returns a Task named name with uid and the annotations given
// by name after the test domain.
func annotated(name, uid string, values map[string]string) Task {
	t := Task{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid), Annotations: map[string]string{}}}
	for k, v := range values {
		t.Annotations[testDomain+"/"+k] = v
	}
	return t
}

func origin(values map[string]string) Origin {
	return Origin{Task: "tasks/agents/t", Annotations: values}
}

func TestOriginKeepsOnlyCorrelationAnnotations(t *testing.T) {
	task := annotated("t", "u1", map[string]string{AnnotationPR: "41", AnnotationEvent: " ", "prompt": "never kept"})
	task.Annotations["kubectl.kubernetes.io/last-applied-configuration"] = "{\"spec\":{\"prompt\":\"secret\"}}"
	task.Annotations["other.example.com/pr"] = "9"
	o, ok := testConfig().originOf(&task, testObservedAt)
	if !ok {
		t.Fatal("no origin")
	}
	want := Origin{Task: "tasks/agents/t", Annotations: map[string]string{AnnotationPR: "41"}, SeenAt: testObservedAt}
	if !reflect.DeepEqual(o, want) {
		t.Errorf("origin = %+v, want %+v", o, want)
	}
	nul := annotated("t", "u1", map[string]string{AnnotationPR: "41\x00", AnnotationComment: "9101\n7"})
	if o, ok := testConfig().originOf(&nul, testObservedAt); ok {
		t.Errorf("origin = %+v, want none: every value holds a control character", o)
	}
	plain := annotated("t", "u1", map[string]string{"prompt": "never kept"})
	if _, ok := testConfig().originOf(&plain, testObservedAt); ok {
		t.Error("a Task with no correlation annotation has an origin")
	}
}

func TestOriginNoteSaysWhenItWasKept(t *testing.T) {
	o := origin(map[string]string{AnnotationPR: "41"})
	if got, want := o.Note(testConfig(), AnnotationPR), "annotation "+testDomain+"/pr=41"; got != want {
		t.Errorf("Note() = %q, want %q", got, want)
	}
	o.remembered, o.SeenAt = true, time.Date(2026, 3, 12, 11, 10, 0, 0, time.UTC)
	checkNote(t, o.Note(testConfig(), AnnotationPR), "pr=41", "read from the Task at 2026-03-12T11:10:00Z",
		"its TaskRecord does not copy annotations")
}

func TestOriginsAndRecordOnlyRunsRuns(t *testing.T) {
	s := Snapshot{
		ObservedAt: testObservedAt,
		Tasks: []Task{
			annotated("listed", "u-listed", map[string]string{AnnotationPR: "41"}),
			annotated("plain", "u-plain", nil),
		},
		Records: []TaskRecord{
			{Spec: TaskRecordSpec{TaskRef: TaskReference{Name: "listed", UID: "u-listed"}}},
			{Spec: TaskRecordSpec{TaskRef: TaskReference{Name: "gone-b", UID: "u-gone-b"}}},
			{Spec: TaskRecordSpec{TaskRef: TaskReference{Name: "gone-a", UID: "u-gone-a"}}},
			{Spec: TaskRecordSpec{TaskRef: TaskReference{Name: "no-uid"}}},
		},
	}
	origins := Origins(testConfig(), s)
	if len(origins) != 1 || origins[runKey("u-listed")].SeenAt != testObservedAt {
		t.Errorf("Origins() = %v, want the listed Task's, seen at the read", origins)
	}
	got := RecordOnlyRuns(testConfig(), s)
	if want := []model.RunKey{runKey("u-gone-a"), runKey("u-gone-b")}; !reflect.DeepEqual(got, want) {
		t.Errorf("RecordOnlyRuns() = %v, want %v", got, want)
	}
}

// A run read with its Task takes the Task's annotations, never a kept
// Origin; one read from its TaskRecord alone takes the kept Origin.
func TestNormalizeTakesTheTasksAnnotationsOverAKeptOrigin(t *testing.T) {
	gh := map[string]string{
		AnnotationSource: OriginGitHub, AnnotationSourceInstance: "github.example.com",
		AnnotationRepository: "example-org/widgets",
	}
	task := annotated("listed", "u-listed", withValue(gh, AnnotationPR, "41"))
	task.Status.Phase = PhaseRunning
	s := Snapshot{
		Tasks: []Task{task},
		Records: []TaskRecord{{
			ObjectMeta: metav1.ObjectMeta{Name: "u-gone"},
			Spec:       TaskRecordSpec{TaskRef: TaskReference{Name: "gone", UID: "u-gone"}, Phase: PhaseSucceeded},
		}},
		Remembered: map[types.UID]Origin{
			"u-listed": origin(withValue(gh, AnnotationPR, "1")),
			"u-gone":   origin(withValue(gh, AnnotationPR, "42")),
		},
	}
	recs, err := Normalize(testConfig(), s)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"u-listed": "/pull/41", "u-gone": "/pull/42"}
	if len(recs.Correlations) != len(want) {
		t.Fatalf("correlations = %v, want one per run", recs.Correlations)
	}
	for _, c := range recs.Correlations {
		uid := c.From.ID[strings.LastIndex(c.From.ID, "/")+1:]
		if !strings.HasSuffix(c.To.ID, want[uid]) {
			t.Errorf("run %s links %s, want the pull request ending %s", uid, c.To, want[uid])
		}
	}
}
