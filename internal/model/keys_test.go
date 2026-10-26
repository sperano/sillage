package model

import "testing"

func TestKeyStrings(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"task", testTaskKey().String(), "vikunja.example.com/tasks/42"},
		{"run by uid", testRunKey("uid-1").String(), "cluster-a/agents/uid/uid-1"},
		{
			"run by pod",
			RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: "pod-1"}.String(),
			"cluster-a/agents/pod/pod-1",
		},
		{"pull request", testPRKey().String(), "github.example.com/example-org/example-repo/pull/12"},
		{"ref", Ref{Kind: RefRun, ID: "x/y"}.String(), "run:x/y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("String() = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestTaskKeyValidate(t *testing.T) {
	tests := []struct {
		name string
		key  TaskKey
		want string
	}{
		{"valid", testTaskKey(), ""},
		{"missing instance", TaskKey{ID: testTaskID}, "instance"},
		{"zero id", TaskKey{Instance: testVikunjaHost}, "positive id"},
		{"negative id", TaskKey{Instance: testVikunjaHost, ID: -1}, "positive id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.key.Validate(), tt.want) })
	}
}

func TestRunKeyValidate(t *testing.T) {
	tests := []struct {
		name string
		key  RunKey
		want string
	}{
		{"valid uid", testRunKey("uid-1"), ""},
		{"valid pod", RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: "pod-1"}, ""},
		{"missing cluster", RunKey{Namespace: testNamespace, UID: "uid-1"}, "cluster and namespace"},
		{"missing namespace", RunKey{Cluster: testCluster, UID: "uid-1"}, "cluster and namespace"},
		{"neither uid nor pod", RunKey{Cluster: testCluster, Namespace: testNamespace}, "exactly one of uid and pod"},
		{
			"both uid and pod",
			RunKey{Cluster: testCluster, Namespace: testNamespace, UID: "uid-1", Pod: "pod-1"},
			"exactly one of uid and pod",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.key.Validate(), tt.want) })
	}
}

func TestPRKeyValidate(t *testing.T) {
	tests := []struct {
		name string
		key  PRKey
		want string
	}{
		{"valid", testPRKey(), ""},
		{"missing instance", PRKey{Repository: testRepository, Number: testPRNum}, "instance"},
		{"missing repository", PRKey{Instance: testGitHubHost, Number: testPRNum}, "repository"},
		{"zero number", PRKey{Instance: testGitHubHost, Repository: testRepository}, "positive number"},
		{"negative number", PRKey{Instance: testGitHubHost, Repository: testRepository, Number: -1}, "positive number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.key.Validate(), tt.want) })
	}
}

func TestRootValidate(t *testing.T) {
	task := testTaskKey()
	badTask := TaskKey{}
	tests := []struct {
		name string
		root Root
		want string
	}{
		{"task", Root{Task: &task}, ""},
		{"no task", Root{}, "task is required"},
		{"invalid task", Root{Task: &badTask}, "task key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.root.Validate(), tt.want) })
	}
}

func TestRefValidate(t *testing.T) {
	tests := []struct {
		name string
		ref  Ref
		want string
	}{
		{"task", Ref{Kind: RefTask, ID: "a"}, ""},
		{"run", Ref{Kind: RefRun, ID: "a"}, ""},
		{"pr", Ref{Kind: RefPR, ID: "a"}, ""},
		{"event", Ref{Kind: RefEvent, ID: "a"}, ""},
		{"unknown kind", Ref{Kind: "comment", ID: "a"}, "unknown kind"},
		{"empty kind", Ref{ID: "a"}, "unknown kind"},
		{"empty id", Ref{Kind: RefRun}, "id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.ref.Validate(), tt.want) })
	}
}

func TestKeyRefs(t *testing.T) {
	podKey := RunKey{Cluster: testCluster, Namespace: testNamespace, Pod: "pod-1"}
	tests := []struct {
		name string
		got  Ref
		want Ref
	}{
		{"task", testTaskKey().Ref(), Ref{Kind: RefTask, ID: "vikunja.example.com/tasks/42"}},
		{"run", testRunKey("uid-1").Ref(), Ref{Kind: RefRun, ID: "cluster-a/agents/uid/uid-1"}},
		{"run by pod", podKey.Ref(), Ref{Kind: RefRun, ID: "cluster-a/agents/pod/pod-1"}},
		{
			"pull request",
			testPRKey().Ref(),
			Ref{Kind: RefPR, ID: "github.example.com/example-org/example-repo/pull/12"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Ref() = %+v, want %+v", tt.got, tt.want)
			}
			if err := tt.got.Validate(); err != nil {
				t.Errorf("Ref().Validate() = %v, want nil", err)
			}
		})
	}
}

func TestSourceValid(t *testing.T) {
	for _, s := range []Source{SourceVikunja, SourceKelos, SourceGitHub} {
		if !s.Valid() {
			t.Errorf("Source(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []Source{"", "gitlab", "Vikunja"} {
		if s.Valid() {
			t.Errorf("Source(%q).Valid() = true, want false", s)
		}
	}
}

func TestRootRef(t *testing.T) {
	task := testTaskKey()
	tests := []struct {
		name string
		root Root
		want Ref
	}{
		{"task", Root{Task: &task}, task.Ref()},
		{"none", Root{}, Ref{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.root.Ref(); got != tt.want {
				t.Errorf("Ref() = %v, want %v", got, tt.want)
			}
		})
	}
}
