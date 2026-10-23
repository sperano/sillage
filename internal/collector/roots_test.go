package collector

import (
	"slices"
	"testing"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

func TestDiscover(t *testing.T) {
	c, err := New(testConfig(), emptySources())
	if err != nil {
		t.Fatal(err)
	}
	snap := kelos.Snapshot{
		Tasks: []kelos.Task{
			task("vikunja-widgets-agent-opus-7", "uid-label", "", taskLabels("7"), 0, 5),
			task("pr-followup-widgets-3-c11", "uid-branch", "kelos/vikunja-8", nil, 0, 5),
			task("vikunja-fu-widgets-9-c12", "uid-name", "", nil, 0, 5),
			task("wiki-fu-widgets-57-c13", "uid-page", "kelos/wiki-57", nil, 0, 5),
			task("unrelated", "uid-none", "main", nil, 0, 5),
		},
		Records: []kelos.TaskRecord{record("gone", "uid-record", taskLabels("10"), 0, 5)},
	}
	got, err := c.Discover(snap)
	if err != nil {
		t.Fatal(err)
	}
	want := map[model.Ref][]string{
		taskRoot(7).Ref():  {"uid-label"},
		taskRoot(8).Ref():  {"uid-branch"},
		taskRoot(9).Ref():  {"uid-name"},
		taskRoot(10).Ref(): {"uid-record"},
		pageRoot(57).Ref(): {"uid-page"},
	}
	if len(got) != len(want) {
		t.Fatalf("Discover() = %d roots %v, want %d", len(got), refsOf(got), len(want))
	}
	for _, d := range got {
		var uids []string
		for _, r := range d.Runs {
			uids = append(uids, r.Key.UID)
		}
		if !slices.Equal(uids, want[d.Root.Ref()]) {
			t.Errorf("root %v: runs %v, want %v", d.Root.Ref(), uids, want[d.Root.Ref()])
		}
	}
	if !slices.IsSortedFunc(refsOf(got), compareRefs) {
		t.Errorf("roots %v are not in ref order", refsOf(got))
	}
}

func TestDiscoverRejectsAnInvalidRead(t *testing.T) {
	c, err := New(testConfig(), emptySources())
	if err != nil {
		t.Fatal(err)
	}
	snap := kelos.Snapshot{Tasks: []kelos.Task{task("no-uid", "", "", nil, 0, 5)}}
	if _, err := c.Discover(snap); err == nil {
		t.Error("Discover() error = nil, want the Task without a UID refused")
	}
}

func TestBranchID(t *testing.T) {
	tests := []struct {
		branch string
		want   int64
		ok     bool
	}{
		{"kelos/vikunja-12", 12, true},
		{"kelos/vikunja-012", 0, false},
		{"kelos/vikunja-0", 0, false},
		{"kelos/vikunja-x", 0, false},
		{"other/vikunja-12", 0, false},
		{"12", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := branchID(tt.branch, taskBranch)
		if got != tt.want || ok != tt.ok {
			t.Errorf("branchID(%q) = %d, %v, want %d, %v", tt.branch, got, ok, tt.want, tt.ok)
		}
	}
}

func refsOf(ds []Discovered) []model.Ref {
	out := make([]model.Ref, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Root.Ref())
	}
	return out
}
