package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func originRun(uid string) model.RunKey {
	return model.RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}
}

// originJSON is an origin naming pull request pr.
func originJSON(pr string) []byte {
	return []byte(`{"task": "tasks/agents/t", "annotations": {"pr": "` + pr + `"}}`)
}

// prOf returns the pr annotation an origin holds.
func prOf(t *testing.T, body []byte) string {
	t.Helper()
	var o struct {
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(body, &o); err != nil {
		t.Fatal(err)
	}
	return o.Annotations["pr"]
}

// The first origin kept of a run stays; a run with none is left out.
func TestKeepOriginsFirstSightWins(t *testing.T) {
	s := newTestStore(t, Retention{})
	ctx := t.Context()
	if err := s.KeepOrigins(ctx, map[model.RunKey][]byte{originRun("u1"): originJSON("41")}); err != nil {
		t.Fatal(err)
	}
	err := s.KeepOrigins(ctx, map[model.RunKey][]byte{originRun("u1"): originJSON("99"), originRun("u2"): originJSON("42")})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.RecallOrigins(ctx, []model.RunKey{originRun("u1"), originRun("u2"), originRun("u3"),
		{Cluster: "cluster-b", Namespace: testNamespace, UID: "u1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || prOf(t, got[originRun("u1")]) != "41" || prOf(t, got[originRun("u2")]) != "42" {
		t.Errorf("RecallOrigins() = %s, want u1's first origin and u2's", got)
	}
}

func TestKeepOriginsRejects(t *testing.T) {
	s := newTestStore(t, Retention{})
	for name, tt := range map[string]struct {
		key  model.RunKey
		body []byte
		want string
	}{
		"a pod-keyed run": {model.RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: "p"}, originJSON("1"), "not keyed by Task UID"},
		"no cluster":      {model.RunKey{Namespace: testNamespace, UID: "u"}, originJSON("1"), "cluster and namespace"},
		"not JSON":        {originRun("u"), []byte("{"), "not JSON"},
		"not an object":   {originRun("u"), []byte(`"pr"`), "check"},
	} {
		err := s.KeepOrigins(t.Context(), map[model.RunKey][]byte{tt.key: tt.body})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", name, err, tt.want)
		}
	}
	if _, err := s.RecallOrigins(t.Context(), []model.RunKey{{Cluster: testCluster, Namespace: testNamespace, Pod: "p"}}); err == nil {
		t.Error("RecallOrigins accepted a pod-keyed run")
	}
}

// Prune keeps an origin while a TaskRecord of its run can still be read,
// however short the retention, and deletes it past both.
func TestPruneOrigins(t *testing.T) {
	s := newTestStore(t, Retention{keep: day})
	ctx := t.Context()
	if err := s.KeepOrigins(ctx, map[model.RunKey][]byte{originRun("u1"): originJSON("41")}); err != nil {
		t.Fatal(err)
	}
	var seen time.Time
	if err := s.pool.QueryRow(ctx, "SELECT first_seen_at FROM run_origins WHERE uid = 'u1'").Scan(&seen); err != nil {
		t.Fatal(err)
	}
	recall := func() int {
		got, err := s.RecallOrigins(ctx, []model.RunKey{originRun("u1")})
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}
	if _, err := s.Prune(ctx, seen.Add(taskRecordTTL-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if recall() != 1 {
		t.Error("pruned an origin whose TaskRecord can still be read")
	}
	if _, err := s.Prune(ctx, seen.Add(taskRecordTTL+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if recall() != 0 {
		t.Error("kept an origin past both the retention and the TaskRecord's life")
	}
}

func TestOriginsCutoff(t *testing.T) {
	now := at(0)
	if got, want := originsCutoff(now.Add(-day), now), now.Add(-taskRecordTTL); !got.Equal(want) {
		t.Errorf("short retention: cutoff %v, want the TaskRecord TTL's %v", got, want)
	}
	long := now.Add(-2 * taskRecordTTL)
	if got := originsCutoff(long, now); !got.Equal(long) {
		t.Errorf("long retention: cutoff %v, want the retention's %v", got, long)
	}
}
