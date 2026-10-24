package reconcile

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/vikunja"
)

func taskEvents(t *testing.T, observed time.Time) model.Records {
	t.Helper()
	snap := testTask(testTaskID, observed)
	snap.Instance = testVikunjaHost
	recs, err := vikunja.Normalize(snap, vikunja.DefaultServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func TestDigestIgnoresWhenRecordsWereRead(t *testing.T) {
	a, b := taskEvents(t, minute(10)), taskEvents(t, minute(20))
	if digest(a) != digest(b) {
		t.Error("two reads of an unchanged task have different digests")
	}
	b.Coverage = []model.Coverage{{Source: model.SourceVikunja, Instance: testVikunjaHost}}
	if digest(a) != digest(b) {
		t.Error("coverage changes the digest")
	}
}

func TestDigestSeesAChange(t *testing.T) {
	a := taskEvents(t, minute(10))
	snap := testTask(testTaskID, minute(20))
	snap.Instance = testVikunjaHost
	snap.Task.Labels = nil
	b, err := vikunja.Normalize(snap, vikunja.DefaultServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	if digest(a) == digest(b) {
		t.Error("a label removed leaves the digest unchanged")
	}
}
