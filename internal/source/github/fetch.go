package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// stateAll asks the pulls list for open and closed (merged or not)
	// pull requests alike.
	stateAll = "all"
	// sortCreated and directionAsc list pull requests oldest first, so a
	// pull request opened during the read lands on the last page instead
	// of shifting the others.
	sortCreated  = "created"
	directionAsc = "asc"
	// filterAll asks the check-runs list for every attempt, re-runs
	// included, not only the latest run of each name.
	filterAll = "all"
	// maxCheckedCommits bounds the commits whose check runs are read for
	// one pull request, one request each: the latest ones, the head
	// included. Older ones are listed in Snapshot.UncheckedSHAs.
	maxCheckedCommits = 250
)

// FetchBranch reads every pull request, open, closed or merged, whose head
// is head and whose base repository is base: none, one or several. They
// come oldest first.
func (c *Client) FetchBranch(ctx context.Context, base Repository, head Branch) ([]Snapshot, error) {
	if err := base.validate(); err != nil {
		return nil, err
	}
	if err := head.validate(); err != nil {
		return nil, err
	}
	query := url.Values{
		"state":     {stateAll},
		"head":      {head.Repository.Owner + ":" + head.Name},
		"sort":      {sortCreated},
		"direction": {directionAsc},
	}
	pulls, err := listAll[PullRequest](ctx, c, base.apiPath()+"/pulls", query)
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	seen := make(map[int]bool)
	for _, pr := range pulls {
		// GitHub ignores a head filter it cannot parse and lists every
		// pull request, so each one is checked here.
		if seen[pr.Number] || !headIs(pr, head) {
			continue
		}
		seen[pr.Number] = true
		snap, err := c.fetchDetails(ctx, base, pr)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

// headIs reports whether pr's head is branch b. A head whose fork was
// deleted has no repository and matches nothing.
func headIs(pr PullRequest, b Branch) bool {
	return pr.Head.Ref == b.Name && pr.Head.Repo != nil &&
		strings.EqualFold(pr.Head.Repo.FullName, b.Repository.String())
}

// FetchPR reads pull request number of repo.
func (c *Client) FetchPR(ctx context.Context, repo Repository, number int) (Snapshot, error) {
	if err := repo.validate(); err != nil {
		return Snapshot{}, err
	}
	if number <= 0 {
		return Snapshot{}, fmt.Errorf("github: pull request %s#%d: a positive number is required", repo, number)
	}
	path := pullPath(repo, number)
	var pr PullRequest
	if err := c.getOne(ctx, path, &pr); err != nil {
		return Snapshot{}, err
	}
	if pr.Number != number {
		return Snapshot{}, fmt.Errorf("github: GET %s: answered with pull request %d", path, pr.Number)
	}
	return c.fetchDetails(ctx, repo, pr)
}

// fetchDetails reads pr's timeline, comments, reviews and the check runs
// of every commit the timeline names.
func (c *Client) fetchDetails(ctx context.Context, repo Repository, pr PullRequest) (Snapshot, error) {
	snap := Snapshot{Instance: c.instance, Repository: repo, PullRequest: pr}
	var err error
	if snap.Timeline, err = listAll[TimelineEvent](ctx, c, timelinePath(repo, pr.Number), nil); err != nil {
		return Snapshot{}, err
	}
	if snap.Comments, err = listAll[IssueComment](ctx, c, commentsPath(repo, pr.Number), nil); err != nil {
		return Snapshot{}, err
	}
	if snap.Reviews, err = listAll[Review](ctx, c, pullPath(repo, pr.Number)+"/reviews", nil); err != nil {
		return Snapshot{}, err
	}
	snap.UncheckedSHAs, snap.SHAs = splitLatest(commitSHAs(snap.Timeline, pr.Head.SHA), maxCheckedCommits)
	for _, sha := range snap.SHAs {
		runs, err := c.fetchCheckRuns(ctx, repo, sha)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Checks = append(snap.Checks, runs...)
	}
	snap.ObservedAt = c.now()
	return snap, nil
}

// commitSHAs returns the commits the timeline names (committed, and the
// new head of each force push), in its order, then head; each once.
func commitSHAs(timeline []TimelineEvent, head string) []string {
	var shas []string
	seen := map[string]bool{head: true}
	add := func(sha string) {
		if sha != "" && !seen[sha] {
			seen[sha] = true
			shas = append(shas, sha)
		}
	}
	for _, ev := range timeline {
		switch ev.Event {
		case timelineCommitted:
			add(ev.SHA)
		case timelineForcePushed:
			add(ev.CommitID)
		}
	}
	if head != "" {
		shas = append(shas, head)
	}
	return shas
}

// splitLatest splits shas into the older ones and the last limit.
func splitLatest(shas []string, limit int) (older, latest []string) {
	cut := max(len(shas)-limit, 0)
	return shas[:cut], shas[cut:]
}

// fetchCheckRuns reads every check run on sha, re-run attempts included.
func (c *Client) fetchCheckRuns(ctx context.Context, repo Repository, sha string) ([]CheckRun, error) {
	var runs []CheckRun
	path := repo.apiPath() + "/commits/" + url.PathEscape(sha) + "/check-runs"
	err := paginate(ctx, c, path, url.Values{"filter": {filterAll}}, func(page checkRunsPage) {
		runs = append(runs, page.CheckRuns...)
	})
	return runs, err
}

func pullPath(repo Repository, number int) string {
	return repo.apiPath() + "/pulls/" + strconv.Itoa(number)
}

func timelinePath(repo Repository, number int) string {
	return repo.apiPath() + "/issues/" + strconv.Itoa(number) + "/timeline"
}

func commentsPath(repo Repository, number int) string {
	return repo.apiPath() + "/issues/" + strconv.Itoa(number) + "/comments"
}
