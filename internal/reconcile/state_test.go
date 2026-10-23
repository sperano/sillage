package reconcile

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/model"
)

func minute(n int) time.Time { return testStart.Add(time.Duration(n) * time.Minute) }

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
