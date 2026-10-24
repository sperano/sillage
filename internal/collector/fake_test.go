package collector

import (
	"testing"
	"time"

	"github.com/sperano/sillage/internal/collector/collectortest"
	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/wiki"
)

const (
	// fixturesDir holds every source's fabricated responses.
	fixturesDir = "../../testdata/fixtures"
	// The fabricated instances the fixtures name.
	testVikunjaHost    = collectortest.VikunjaHost
	testGitHubHost     = collectortest.GitHubHost
	testWikiHost       = collectortest.WikiHost
	testCluster        = collectortest.Cluster
	testNamespace      = collectortest.Namespace
	testWorkspace      = collectortest.Workspace
	testAgent          = collectortest.Agent
	testServiceAccount = collectortest.ServiceAccount
	testGitHubAPIPath  = collectortest.GitHubAPIPath
	testToken          = collectortest.Token
	testWikiUser       = collectortest.WikiUser
)

var (
	testRepo = collectortest.Repo
	// testNow is the collector's clock: after every fixture.
	testNow = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
)

// storyDirs names, under fixturesDir, the responses of each source a
// story reads; an empty one is a source the story must not read.
type storyDirs = collectortest.Dirs

func testConfig() Config {
	return Config{
		VikunjaInstance:       testVikunjaHost,
		VikunjaServiceAccount: testServiceAccount,
		VikunjaAgentUsers:     []string{testAgent},
		Kelos:                 collectortest.KelosConfig(),
		GitHubInstance:        testGitHubHost,
		WikiInstance:          testWikiHost,
		WikiAgentUsers:        []int64{testWikiUser},
		Workspaces:            map[string]github.Repository{testWorkspace: testRepo},
		KelosTaskTTL:          DefaultKelosTaskTTL,
	}
}

// newStoryCollector returns a collector of the clients of every source,
// each answered from dirs; any other request fails the test.
func newStoryCollector(t *testing.T, dirs storyDirs) *Collector {
	t.Helper()
	cl := collectortest.New(t, fixturesDir, dirs, collectortest.Report{Elsewhere: t.Errorf, Broken: t.Errorf})
	c, err := New(testConfig(), Sources{Vikunja: cl.Vikunja, Kelos: cl.Kelos, GitHub: cl.GitHub, Wiki: cl.Wiki})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return testNow }
	return c
}

// taskRoot and pageRoot are the fixtures' roots.
func taskRoot(id int64) model.Root {
	return model.Root{Task: &model.TaskKey{Instance: testVikunjaHost, ID: id}}
}

func pageRoot(id int64) model.Root {
	return model.Root{Page: &model.PageKey{Instance: testWikiHost, ID: id, Locale: wiki.DefaultLocale}}
}
