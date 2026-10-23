package model

import (
	"slices"
	"testing"
	"time"
)

type eventCase = struct {
	name   string
	mutate func(*Event)
	want   string
}

func runEventCases(t *testing.T, tests []eventCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEvent("c-1")
			tt.mutate(&e)
			checkErr(t, e.Validate(), tt.want)
		})
	}
}

func TestEventValidate(t *testing.T) {
	tests := []eventCase{
		{"valid", func(*Event) {}, ""},
		{"valid without source time", func(e *Event) { e.SourceTime = nil }, ""},
		{"unknown source", func(e *Event) { e.Source = "gitlab" }, "source, instance and id"},
		{"missing instance", func(e *Event) { e.Instance = "" }, "source, instance and id"},
		{"missing id", func(e *Event) { e.ID = "" }, "source, instance and id"},
		{"unknown id kind", func(e *Event) { e.IDKind = "uuid" }, "unknown id kind"},
		{"empty id kind", func(e *Event) { e.IDKind = "" }, "unknown id kind"},
		{
			"fingerprint kind with a source id",
			func(e *Event) { e.IDKind = IDFingerprint },
			"is not a fingerprint",
		},
		{
			"fingerprint kind with a fingerprint",
			func(e *Event) {
				e.IDKind = IDFingerprint
				e.ID = Fingerprint(at(0), "tasks/42", "done", "")
			},
			"",
		},
		{"pr.merged from vikunja", func(e *Event) { e.Kind = KindPRMerged }, "cannot come from vikunja"},
		{"unknown kind", func(e *Event) { e.Kind = "task.exploded" }, "unknown kind"},
		{"missing observedAt", func(e *Event) { e.ObservedAt = time.Time{} }, "observed time"},
	}
	runEventCases(t, tests)
}

func TestEventValidateLastObserved(t *testing.T) {
	observation := func(e *Event) {
		e.Kind, e.SourceTime = KindTaskBucketObserved, nil
	}
	tests := []eventCase{
		{"observation seen again", func(e *Event) {
			observation(e)
			e.LastObservedAt = timePtr(at(20))
		}, ""},
		{"observation seen once at the same time", func(e *Event) {
			observation(e)
			e.LastObservedAt = timePtr(e.ObservedAt)
		}, ""},
		{"before the first observation", func(e *Event) {
			observation(e)
			e.LastObservedAt = timePtr(at(9))
		}, "before the observed time"},
		{"on a comment", func(e *Event) {
			e.SourceTime = nil
			e.LastObservedAt = timePtr(at(20))
		}, "only a state seen by polling"},
		{"with a source time", func(e *Event) {
			observation(e)
			e.SourceTime = timePtr(at(5))
			e.LastObservedAt = timePtr(at(20))
		}, "only a state seen by polling"},
	}
	runEventCases(t, tests)
}

func TestEventKindObserved(t *testing.T) {
	for kind := range kindSources {
		want := kind == KindTaskBucketObserved || kind == KindTaskLabelObserved || kind == KindTaskAssigneeObserved
		if got := kind.Observed(); got != want {
			t.Errorf("%s.Observed() = %v, want %v", kind, got, want)
		}
	}
}

func TestEventValidateParts(t *testing.T) {
	badTask := TaskKey{}
	badPage := PageKey{}
	badRun := RunKey{}
	badPR := PRKey{}
	tests := []eventCase{
		{"login with unknown kind", func(e *Event) { e.Actor = Account{Login: "alice", Kind: ActorUnknown} }, "actor"},
		{"person without login", func(e *Event) { e.Actor = Account{Kind: ActorPerson} }, "actor"},
		{"unknown actor kind", func(e *Event) { e.Actor = Account{Login: "alice", Kind: "robot"} }, "actor"},
		{"empty actor kind", func(e *Event) { e.Actor = Account{Login: "alice"} }, "actor"},
		{"empty actor", func(e *Event) { e.Actor = Account{} }, "actor"},
		{"unknown actor without login", func(e *Event) { e.Actor = Account{Kind: ActorUnknown} }, ""},
		{"agent actor", func(e *Event) { e.Actor = Account{Login: "bot", Kind: ActorAgent} }, ""},
		{"service actor", func(e *Event) { e.Actor = Account{Login: "svc", Kind: ActorService} }, ""},
		{
			"valid executor",
			func(e *Event) { e.Executor = &Executor{Name: "coder", Kind: ActorAgent} },
			"",
		},
		{
			"executor without name",
			func(e *Event) { e.Executor = &Executor{Kind: ActorAgent} },
			"executor name and kind",
		},
		{
			"executor with unknown kind",
			func(e *Event) { e.Executor = &Executor{Name: "coder", Kind: "robot"} },
			"executor name and kind",
		},
		{"invalid related task", func(e *Event) { e.Related.Task = &badTask }, "task key"},
		{"invalid related page", func(e *Event) { e.Related.Page = &badPage }, "page key"},
		{"invalid related run", func(e *Event) { e.Related.Run = &badRun }, "run key"},
		{"invalid related pr", func(e *Event) { e.Related.PR = &badPR }, "pull request key"},
		{"no related keys", func(e *Event) { e.Related = Related{} }, ""},
		{"evidence without source", func(e *Event) { e.Evidence.Source = "" }, "evidence needs"},
		{"evidence with unknown source", func(e *Event) { e.Evidence.Source = "gitlab" }, "evidence needs"},
		{"evidence without instance", func(e *Event) { e.Evidence.Instance = "" }, "evidence needs"},
		{"evidence without ref", func(e *Event) { e.Evidence.Ref = "" }, "evidence needs"},
		{"evidence without note", func(e *Event) { e.Evidence.Note = "" }, ""},
	}
	runEventCases(t, tests)
}

func TestEventValidateNamesEvent(t *testing.T) {
	e := testEvent("c-1")
	e.ObservedAt = time.Time{}
	checkErr(t, e.Validate(), "event vikunja/vikunja.example.com/c-1:")
}

func TestEventValidateKindSources(t *testing.T) {
	sources := []Source{SourceVikunja, SourceKelos, SourceGitHub, SourceWiki}
	for kind, allowed := range kindSources {
		for _, source := range sources {
			t.Run(string(kind)+"/"+string(source), func(t *testing.T) {
				e := testEvent("c-1")
				e.Kind, e.Source = kind, source
				want := "cannot come from " + string(source)
				if slices.Contains(allowed, source) {
					want = ""
				}
				checkErr(t, e.Validate(), want)
			})
		}
	}
}

func TestEventTime(t *testing.T) {
	sourceTime, observed := at(5), at(10)
	tests := []struct {
		name       string
		sourceTime *time.Time
		want       time.Time
	}{
		{"source time when set", &sourceTime, sourceTime},
		{"observed time otherwise", nil, observed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEvent("c-1")
			e.SourceTime, e.ObservedAt = tt.sourceTime, observed
			if got := e.Time(); !got.Equal(tt.want) {
				t.Errorf("Time() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventKeyAndRef(t *testing.T) {
	e := testEvent("c-1")
	const wantKey = "vikunja/vikunja.example.com/c-1"
	if got := e.Key(); got != wantKey {
		t.Errorf("Key() = %q, want %q", got, wantKey)
	}
	if got, want := e.Ref(), (Ref{Kind: RefEvent, ID: wantKey}); got != want {
		t.Errorf("Ref() = %+v, want %+v", got, want)
	}
}

func TestEventNormalize(t *testing.T) {
	e := testEvent("c-1")
	e.SourceTime = timePtr(atEST(5))
	e.ObservedAt = atEST(10)
	e.normalize()
	if got := e.SourceTime.Location(); got != time.UTC {
		t.Errorf("SourceTime location = %v, want UTC", got)
	}
	if got := e.ObservedAt.Location(); got != time.UTC {
		t.Errorf("ObservedAt location = %v, want UTC", got)
	}
	if !e.SourceTime.Equal(at(5)) || !e.ObservedAt.Equal(at(10)) {
		t.Errorf("normalize changed the instants: %v, %v", e.SourceTime, e.ObservedAt)
	}

	e.SourceTime = nil
	e.normalize()
	if e.SourceTime != nil {
		t.Errorf("normalize set a nil SourceTime to %v", e.SourceTime)
	}

	e.LastObservedAt = timePtr(atEST(20))
	e.normalize()
	if got := e.LastObservedAt.Location(); got != time.UTC || !e.LastObservedAt.Equal(at(20)) {
		t.Errorf("LastObservedAt = %v, want %v in UTC", e.LastObservedAt, at(20))
	}
}
