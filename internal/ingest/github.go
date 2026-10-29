package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/sperano/sillage/internal/collector"
	"github.com/sperano/sillage/internal/model"
)

const (
	// githubSignatureHeader carries "sha256=" and the hex HMAC-SHA256 of
	// the body.
	githubSignatureHeader = "X-Hub-Signature-256"
	githubSignaturePrefix = "sha256="
	// githubDeliveryHeader carries the delivery's id, which a redelivery
	// reuses, and githubEventHeader its event.
	githubDeliveryHeader = "X-GitHub-Delivery"
	githubEventHeader    = "X-GitHub-Event"
	// githubPing is the event GitHub sends when a hook is created.
	githubPing = "ping"
	// maxDeliveryIDLength bounds the delivery id kept (a GUID is 36).
	maxDeliveryIDLength = 128
	// maxEventLength bounds the event name kept (GitHub's longest is
	// under 40).
	maxEventLength = 64
)

var (
	errNoDeliveryID   = errors.New("the delivery has no " + githubDeliveryHeader + " or " + githubEventHeader + " header")
	errLongDeliveryID = fmt.Errorf("the delivery's %s header is longer than %d bytes", githubDeliveryHeader,
		maxDeliveryIDLength)
	errLongEvent = fmt.Errorf("the delivery's %s header is longer than %d bytes", githubEventHeader, maxEventLength)
)

// githubPR is a pull request as the payloads name it.
type githubPR struct {
	Number int `json:"number"`
	Head   struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// githubPayload is the part of a GitHub delivery sillage reads: the
// repository, and the pull requests and branches its event names
// (pull_request, pull_request_review, pull_request_review_comment,
// issue_comment on a pull request, check_run, check_suite).
type githubPayload struct {
	Repository *struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest *githubPR `json:"pull_request"`
	Issue       *struct {
		Number      int             `json:"number"`
		PullRequest json.RawMessage `json:"pull_request"`
	} `json:"issue"`
	CheckRun *struct {
		CheckSuite struct {
			HeadBranch string `json:"head_branch"`
		} `json:"check_suite"`
		PullRequests []githubPR `json:"pull_requests"`
	} `json:"check_run"`
	CheckSuite *struct {
		HeadBranch   string     `json:"head_branch"`
		PullRequests []githubPR `json:"pull_requests"`
	} `json:"check_suite"`
}

// branches returns the branches and pull request numbers p names.
func (p githubPayload) branches() (branches []string, numbers []int) {
	add := func(prs ...githubPR) {
		for _, pr := range prs {
			branches = append(branches, pr.Head.Ref)
			numbers = append(numbers, pr.Number)
		}
	}
	if p.PullRequest != nil {
		add(*p.PullRequest)
	}
	if p.Issue != nil && len(p.Issue.PullRequest) > 0 && string(p.Issue.PullRequest) != "null" {
		numbers = append(numbers, p.Issue.Number)
	}
	if p.CheckRun != nil {
		branches = append(branches, p.CheckRun.CheckSuite.HeadBranch)
		add(p.CheckRun.PullRequests...)
	}
	if p.CheckSuite != nil {
		branches = append(branches, p.CheckSuite.HeadBranch)
		add(p.CheckSuite.PullRequests...)
	}
	return branches, numbers
}

// githubParser reads the deliveries of the GitHub instance.
type githubParser struct {
	cfg   collector.Config
	store PullRequestRoots
}

// GitHubRoots returns the roots a GitHub delivery of event with body
// names, as POST /ingest/github reads them: none for one that names none
// or whose body is not JSON. Only st failing is an error. The collector
// loop reads with it the roots of a delivery GitHub's delivery log shows
// sillage did not receive.
func GitHubRoots(ctx context.Context, cfg collector.Config, st PullRequestRoots, event string, body []byte) (
	[]model.Root, error) {
	roots, _, err := githubParser{cfg: cfg, store: st}.roots(ctx, event, body)
	if errors.Is(err, errNotJSON) {
		return nil, nil
	}
	return roots, err
}

func (githubParser) signature(h http.Header) (prefix, value string) {
	return githubSignaturePrefix, h.Get(githubSignatureHeader)
}

func (githubParser) identify(h http.Header, _ []byte) (id, event string, err error) {
	id, event = h.Get(githubDeliveryHeader), h.Get(githubEventHeader)
	switch {
	case id == "" || event == "":
		return "", "", errNoDeliveryID
	case len(id) > maxDeliveryIDLength:
		return "", "", errLongDeliveryID
	case len(event) > maxEventLength:
		return "", "", errLongEvent
	}
	return id, event, nil
}

// roots returns the roots of the branches the delivery names, by the
// collector's branch conventions, and the stored roots whose stories
// hold the pull requests it names.
func (g githubParser) roots(ctx context.Context, event string, body []byte) ([]model.Root, string, error) {
	if event == githubPing {
		return nil, "GitHub's ping, sent when a hook is created, names no root", nil
	}
	var p githubPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", errNotJSON
	}
	branches, numbers := p.branches()
	var found rootSet
	for _, b := range branches {
		if root, ok := g.cfg.RootOfBranch(b); ok {
			found.add(root)
		}
	}
	if p.Repository != nil && p.Repository.FullName != "" {
		slices.Sort(numbers)
		for _, n := range slices.Compact(numbers) {
			if err := g.storedRoots(ctx, &found, p.Repository.FullName, n); err != nil {
				return nil, "", err
			}
		}
	}
	if len(found.roots) == 0 {
		return nil, "the event names no task or page branch, and no pull request a stored story holds", nil
	}
	return found.roots, "", nil
}

// storedRoots adds the stored roots whose stories hold pull request n of
// repository.
func (g githubParser) storedRoots(ctx context.Context, found *rootSet, repository string, n int) error {
	if n <= 0 {
		return nil
	}
	sums, err := g.store.RootsOfPullRequest(ctx, model.PRKey{Instance: g.cfg.GitHubInstance, Repository: repository, Number: n})
	if err != nil {
		return &storeError{op: "finding the stories of a pull request", err: err}
	}
	for _, s := range sums {
		found.add(s.Root)
	}
	return nil
}
