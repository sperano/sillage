package reconcile

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

func minute(n int) time.Time { return testStart.Add(time.Duration(n) * time.Minute) }

func TestDropRetiredSources(t *testing.T) {
	st := newRootState(testRoot())
	st.Sources["vikunja/"+testVikunjaHost] = &sourceState{Source: model.SourceVikunja, Instance: testVikunjaHost}
	st.Sources["wiki/b.example.com"] = &sourceState{Source: "wiki", Instance: "b.example.com"}
	st.Sources["wiki/a.example.com"] = &sourceState{Source: "wiki", Instance: "a.example.com"}
	got := st.dropRetiredSources()
	if want := []string{"wiki/a.example.com", "wiki/b.example.com"}; !slices.Equal(got, want) {
		t.Errorf("dropped = %v, want %v", got, want)
	}
	if len(st.Sources) != 1 || st.Sources["vikunja/"+testVikunjaHost] == nil {
		t.Errorf("sources left = %v, want only the Vikunja one", slices.Sorted(maps.Keys(st.Sources)))
	}
	if cov := st.coverage(); len(cov) != 1 || cov[0].Source != model.SourceVikunja {
		t.Errorf("coverage = %+v, want only the Vikunja source's", cov)
	}
}

func TestSourceStateGaps(t *testing.T) {
	cov := model.Coverage{Source: model.SourceVikunja, Instance: testVikunjaHost}
	var st sourceState
	st.fail("before any read")
	if st.Open != nil {
		t.Fatalf("a failure before any success opened %+v, want nothing: nothing is known yet", st.Open)
	}
	st.succeed(minute(1), cov)
	st.fail("first")
	st.fail("second")
	st.succeed(minute(3), cov)
	want := []model.Gap{{Start: minute(1), End: minute(3), Reason: "first"}}
	if len(st.Gaps) != 1 || st.Gaps[0] != want[0] {
		t.Errorf("gaps = %v, want %v", st.Gaps, want)
	}
	if !st.FirstSuccess.Equal(minute(1)) || !st.LastSuccess.Equal(minute(3)) {
		t.Errorf("successes %v, %v, want minutes 1 and 3", st.FirstSuccess, st.LastSuccess)
	}
}

func TestSourceStateOpenAfter(t *testing.T) {
	cov := model.Coverage{Source: model.SourceKelos, Instance: testCluster}
	tests := []struct {
		name      string
		lastCycle time.Time
		want      time.Time
	}{
		{"cycle unknown", time.Time{}, minute(1)},
		{"cycle after the read", minute(4), minute(4)},
		{"cycle before the read", minute(0), minute(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var st sourceState
			st.succeed(minute(1), cov)
			st.openAfter(tt.lastCycle, "restarted")
			if st.Open == nil || !st.Open.Start.Equal(tt.want) {
				t.Errorf("open gap = %+v, want one from %v", st.Open, tt.want)
			}
		})
	}
	var never sourceState
	never.openAfter(minute(4), "restarted")
	if never.Open != nil {
		t.Errorf("a source never read opened %+v on restart, want nothing", never.Open)
	}
}

func TestSourceStateCoverage(t *testing.T) {
	read := model.Gap{Start: minute(0), End: minute(1), Reason: "read"}
	var st sourceState
	st.Source, st.Instance = model.SourceKelos, testCluster
	st.succeed(minute(2), model.Coverage{Gaps: []model.Gap{read}, Notes: []string{"a note"}})
	st.fail("down")
	st.succeed(minute(5), model.Coverage{Gaps: []model.Gap{read}, Notes: []string{"a note"}})
	st.fail("down again")
	cov := st.coverage()
	if err := cov.Validate(); err != nil {
		t.Fatal(err)
	}
	if !cov.LastSuccessfulRead.Equal(minute(5)) || len(cov.Gaps) != 2 || cov.Gaps[0] != read ||
		!cov.Gaps[1].End.Equal(minute(5)) || len(cov.Notes) != 1 {
		t.Errorf("coverage = %+v, want the last success, the read's gap and the closed one, not the open one", cov)
	}
}

func TestRootStateCloneIsDeep(t *testing.T) {
	st := newRootState(testRoot())
	st.source(model.Coverage{Source: model.SourceVikunja, Instance: testVikunjaHost}).succeed(minute(1), model.Coverage{})
	c := st.clone()
	c.source(model.Coverage{Source: model.SourceVikunja, Instance: testVikunjaHost}).fail("down")
	if st.Sources["vikunja/"+testVikunjaHost].Open != nil {
		t.Error("changing the clone changed the original")
	}
}

func TestRootStateCloneCopiesTheGitHubSchedule(t *testing.T) {
	read := minute(1)
	st := newRootState(testRoot())
	st.GitHubRead, st.ETags = &read, github.Validators{"/repos/acme/widgets/pulls": `W/"1"`}
	c := st.clone()
	*c.GitHubRead = minute(2)
	c.ETags["/repos/acme/widgets/pulls"] = `W/"2"`
	if !st.GitHubRead.Equal(read) || st.ETags["/repos/acme/widgets/pulls"] != `W/"1"` {
		t.Error("changing the clone's GitHub schedule changed the original's")
	}
}

func TestUnchangedKeepsWhatTheLastReadLeftUnknown(t *testing.T) {
	var st sourceState
	st.succeed(minute(1), model.Coverage{Notes: []string{"checks forbidden"}})
	st.fail("down")
	st.unchanged(minute(3))
	if st.Open != nil || len(st.Gaps) != 1 || !st.LastSuccess.Equal(minute(3)) {
		t.Errorf("after an unchanged read, state = %+v: want the gap closed there", st)
	}
	if len(st.Notes) != 1 || st.Notes[0] != "checks forbidden" {
		t.Errorf("notes = %v, want the last read's kept", st.Notes)
	}
}

func TestKelosSinceIsTheConfiguredClusters(t *testing.T) {
	const otherCluster = "cluster-old"
	st := newRootState(testRoot())
	first := minute(1)
	st.source(model.Coverage{Source: model.SourceKelos, Instance: otherCluster}).succeed(first, model.Coverage{})
	if got := st.kelosSince(testCluster); !got.IsZero() {
		t.Errorf("kelosSince(%s) = %v, want zero: only %s was read", testCluster, got, otherCluster)
	}
	if got := st.kelosSince(otherCluster); !got.Equal(first) {
		t.Errorf("kelosSince(%s) = %v, want %v", otherCluster, got, first)
	}
}
