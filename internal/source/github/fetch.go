package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	return c.fetchBranch(ctx, base, head, nil)
}

// fetchBranch is FetchBranch, keeping the ETags of its pulls list and of
// each pull request's timeline and head check runs in t unless t is nil.
func (c *Client) fetchBranch(ctx context.Context, base Repository, head Branch, t *tagger) ([]Snapshot, error) {
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
	pulls, err := listTagged[PullRequest](ctx, c, base.apiPath()+"/pulls", query, t)
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
		snap, err := c.fetchDetails(ctx, base, pr, t)
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
	return c.fetchDetails(ctx, repo, pr, nil)
}

// fetchDetails reads pr's timeline, comments, inline review comments,
// reviews and the check runs of every commit the timeline names. A 403 or
// 404 on the inline review comments or on a commit's check runs is kept
// in Snapshot.Refused instead of failing the read; a repository whose
// check runs were refused 403 is not asked again for checkRunsRefusalTTL. A non-nil t keeps the
// ETags of the timeline and of the head's check runs.
func (c *Client) fetchDetails(ctx context.Context, repo Repository, pr PullRequest, t *tagger) (Snapshot, error) {
	snap := Snapshot{Instance: c.instance, Repository: repo, PullRequest: pr}
	var err error
	if snap.Timeline, err = listTagged[TimelineEvent](ctx, c, timelinePath(repo, pr.Number), nil, t); err != nil {
		return Snapshot{}, err
	}
	if snap.Comments, err = listAll[Comment](ctx, c, commentsPath(repo, pr.Number), nil); err != nil {
		return Snapshot{}, err
	}
	snap.ReviewComments, err = listAll[Comment](ctx, c, reviewCommentsPath(repo, pr.Number), nil)
	if err = snap.refuse(err, ResourceReviewComments, ""); err != nil {
		return Snapshot{}, err
	}
	if snap.Reviews, err = listAll[Review](ctx, c, pullPath(repo, pr.Number)+"/reviews", nil); err != nil {
		return Snapshot{}, err
	}
	snap.UncheckedSHAs, snap.SHAs = splitLatest(commitSHAs(snap.Timeline, pr.Head.SHA), maxCheckedCommits)
	for _, sha := range snap.SHAs {
		var head *tagger
		if sha == pr.Head.SHA {
			head = t
		}
		if err := c.readCheckRuns(ctx, &snap, sha, head); err != nil {
			return Snapshot{}, err
		}
	}
	snap.ObservedAt = c.now()
	return snap, nil
}

// readCheckRuns adds the check runs of sha to snap.Checks, keeping the
// list's ETag in head unless it is nil. A repository whose check runs
// were refused 403 within checkRunsRefusalTTL is not asked again: the refusal
// is added to snap.Refused as Remembered. A 403 or 404 GitHub just
// answered is added instead of failing the read, and only a 403 is
// remembered; any other error is returned.
func (c *Client) readCheckRuns(ctx context.Context, snap *Snapshot, sha string, head *tagger) error {
	repo := snap.Repository
	if code, ok := c.rememberedCheckRunsRefusal(repo); ok {
		// A list not asked for has no ETag to compare.
		head.incomplete()
		snap.Refused = append(snap.Refused, Refusal{
			Resource: ResourceCheckRuns, SHA: sha, Path: checkRunsPath(repo, sha), Code: code, Remembered: true,
		})
		return nil
	}
	runs, err := c.fetchCheckRuns(ctx, repo, sha, head)
	if err != nil {
		// A refused list has no ETag to compare.
		head.incomplete()
	}
	n := len(snap.Refused)
	if err = snap.refuse(err, ResourceCheckRuns, sha); err != nil {
		return err
	}
	// Only a 403 is repository-wide (the credential cannot read checks
	// there); a 404 is usually one missing commit, and must not hide the
	// others.
	if len(snap.Refused) > n && snap.Refused[n].Code == http.StatusForbidden {
		c.rememberCheckRunsRefusal(repo, snap.Refused[n].Code)
	}
	snap.Checks = append(snap.Checks, runs...)
	return nil
}

// refuse keeps err, a sub-read of resource, in s.Refused when GitHub
// answered it 403 or 404, and returns any other error. A rate limit is a
// *RateLimitError, never refused.
func (s *Snapshot) refuse(err error, resource, sha string) error {
	se, ok := errors.AsType[*StatusError](err)
	if !ok || (se.Code != http.StatusForbidden && se.Code != http.StatusNotFound) {
		return err
	}
	s.Refused = append(s.Refused, Refusal{Resource: resource, SHA: sha, Path: se.Path, Code: se.Code})
	return nil
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

// fetchCheckRuns reads every check run on sha, re-run attempts included,
// keeping the list's ETag in t unless t is nil.
func (c *Client) fetchCheckRuns(ctx context.Context, repo Repository, sha string, t *tagger) ([]CheckRun, error) {
	var runs []CheckRun
	err := paginate(ctx, c, checkRunsPath(repo, sha), url.Values{"filter": {filterAll}}, t, func(page checkRunsPage) {
		runs = append(runs, page.CheckRuns...)
	})
	return runs, err
}

// checkRunsPath is the REST path of the check runs of commit sha.
func checkRunsPath(repo Repository, sha string) string {
	return repo.apiPath() + "/commits/" + url.PathEscape(sha) + "/check-runs"
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

func reviewCommentsPath(repo Repository, number int) string {
	return pullPath(repo, number) + "/comments"
}
