package model

import (
	"strings"
	"testing"
	"time"
)

// Fabricated hosts and names shared by the tests of this package.
const (
	testVikunjaHost = "vikunja.example.com"
	testWikiHost    = "wiki.example.com"
	testGitHubHost  = "github.example.com"
	testCluster     = "cluster-a"
	testNamespace   = "agents"
	testRepository  = "example-org/example-repo"
	testBranch      = "kelos/task-42"
	testSHA         = "0123456789abcdef0123456789abcdef01234567"
	testLocale      = "en"

	testTaskID  int64 = 42
	testPageID  int64 = 7
	testCheckID int64 = 1001
	testPRNum         = 12
)

// testEST is a fixed -05:00 zone, so tests can tell a converted time from
// one left as it was given.
var testEST = time.FixedZone("EST", -5*60*60)

// testBase is a fixed instant in UTC that the tests offset from.
var testBase = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)

// at returns testBase plus the given number of minutes, in UTC.
func at(minutes int) time.Time {
	return testBase.Add(time.Duration(minutes) * time.Minute)
}

// atEST returns the same instant as at(minutes), expressed in testEST.
func atEST(minutes int) time.Time { return at(minutes).In(testEST) }

func timePtr(t time.Time) *time.Time { return &t }

// checkErr fails unless err is nil when want is empty, or an error whose
// text contains want otherwise.
func checkErr(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("got error %q, want none", err)
	case want != "" && err == nil:
		t.Errorf("got no error, want one containing %q", want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Errorf("got error %q, want one containing %q", err, want)
	}
}

func testTaskKey() TaskKey { return TaskKey{Instance: testVikunjaHost, ID: testTaskID} }

func testPageKey() PageKey {
	return PageKey{Instance: testWikiHost, ID: testPageID, Locale: testLocale}
}

func testRunKey(uid string) RunKey {
	return RunKey{Cluster: testCluster, Namespace: testNamespace, UID: uid}
}

func testPRKey() PRKey {
	return PRKey{Instance: testGitHubHost, Repository: testRepository, Number: testPRNum}
}

func testEvidence() Evidence {
	return Evidence{Source: SourceVikunja, Instance: testVikunjaHost, Ref: "/api/v1/tasks/42/comments"}
}

// testEvent returns a valid comment event from Vikunja, observed at minute
// 10 with a source time at minute 5.
func testEvent(id string) Event {
	task := testTaskKey()
	return Event{
		Source:     SourceVikunja,
		Instance:   testVikunjaHost,
		ID:         id,
		IDKind:     IDSource,
		Kind:       KindComment,
		SourceTime: timePtr(at(5)),
		ObservedAt: at(10),
		Actor:      Account{Login: "alice", Kind: ActorPerson},
		Related:    Related{Task: &task},
		Evidence:   testEvidence(),
	}
}

// testRun returns a valid, finished run keyed by uid, from minute 0 to 30.
func testRun(uid string) Run {
	return Run{
		Key:      testRunKey(uid),
		TaskName: "task-42",
		Agent:    "coder",
		Role:     RoleImplementation,
		Interval: Interval{Start: KnownAt(at(0)), End: KnownAt(at(30))},
		Outcome:  OutcomeSucceeded,
		Evidence: []Evidence{{Source: SourceKelos, Instance: testCluster, Ref: "tasks/task-42"}},
	}
}

// testCheck returns a valid, successful check from minute 1 to 4.
func testCheck(id int64, sha string) CheckRun {
	return CheckRun{
		ID:         id,
		SHA:        sha,
		Name:       "build",
		Interval:   Interval{Start: KnownAt(at(1)), End: KnownAt(at(4))},
		Conclusion: CheckSuccess,
	}
}

// testPullRequest returns a valid pull request with one successful check.
func testPullRequest() PullRequest {
	return PullRequest{
		Key:    testPRKey(),
		Branch: testBranch,
		Checks: []CheckRun{testCheck(testCheckID, testSHA)},
	}
}

// testCorrelation returns a valid identifier correlation from a run to a
// task.
func testCorrelation() Correlation {
	return Correlation{
		From:       testRunKey("uid-1").Ref(),
		To:         testTaskKey().Ref(),
		Confidence: ConfidenceIdentifier,
		Evidence:   []Evidence{testEvidence()},
	}
}

// testCoverage returns a valid coverage of the Vikunja instance, read up to
// minute 60, with no gaps.
func testCoverage() Coverage {
	return Coverage{
		Source:             SourceVikunja,
		Instance:           testVikunjaHost,
		LastSuccessfulRead: timePtr(at(60)),
	}
}

func testRoot() Root {
	task := testTaskKey()
	return Root{Task: &task}
}
