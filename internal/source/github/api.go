package github

import "time"

// The types below decode GitHub's REST JSON. They keep only the fields
// sillage reads; GitHub sends more.

// User is a GitHub account as embedded in other records. Type is "User",
// "Bot" or "Organization".
type User struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// Repo is a repository as embedded in a pull request's base and head. It is
// null in a head whose fork was deleted.
type Repo struct {
	FullName string `json:"full_name"`
}

// Ref is a pull request's base or head.
type Ref struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo *Repo  `json:"repo"`
}

// PullRequest is a pull request as GET /repos/{owner}/{repo}/pulls returns
// it. MergedAt is nil until it is merged, ClosedAt until it is closed.
type PullRequest struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	State     string     `json:"state"`
	User      *User      `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	MergedAt  *time.Time `json:"merged_at"`
	Head      Ref        `json:"head"`
	Base      Ref        `json:"base"`
}

// Label is a label as embedded in a labeled timeline event.
type Label struct {
	Name string `json:"name"`
}

// Team is a team as embedded in a review request.
type Team struct {
	Slug string `json:"slug"`
}

// TimelineEvent is one item of GET /repos/{owner}/{repo}/issues/{number}/
// timeline. Its fields depend on Event: "committed" items have SHA and no
// ID, actor or time; "labeled" items have Label; review request items have
// RequestedReviewer or RequestedTeam.
type TimelineEvent struct {
	ID                int64     `json:"id"`
	Event             string    `json:"event"`
	Actor             *User     `json:"actor"`
	CreatedAt         time.Time `json:"created_at"`
	CommitID          string    `json:"commit_id"`
	SHA               string    `json:"sha"`
	Label             *Label    `json:"label"`
	RequestedReviewer *User     `json:"requested_reviewer"`
	RequestedTeam     *Team     `json:"requested_team"`
}

// The timeline event names sillage reads; the timeline has many more.
const (
	timelineClosed               = "closed"
	timelineMerged               = "merged"
	timelineReopened             = "reopened"
	timelineReviewRequested      = "review_requested"
	timelineReviewRequestRemoved = "review_request_removed"
	timelineLabeled              = "labeled"
	timelineCommitted            = "committed"
	timelineForcePushed          = "head_ref_force_pushed"
)

// Comment is a pull request comment: a conversation comment, from GET
// /repos/{owner}/{repo}/issues/{number}/comments, or an inline review
// comment on the diff, from GET /repos/{owner}/{repo}/pulls/{number}/
// comments.
type Comment struct {
	ID   int64  `json:"id"`
	User *User  `json:"user"`
	Body string `json:"body"`
	// CreatedAt is, for a comment written in a review, when it was
	// drafted, not when the review made it visible.
	CreatedAt time.Time `json:"created_at"`
	// ReviewID is the review an inline comment belongs to; 0 for a
	// conversation comment.
	ReviewID int64 `json:"pull_request_review_id"`
}

// Review is a pull request review, from GET /repos/{owner}/{repo}/pulls/
// {number}/reviews. SubmittedAt is nil for a pending review, which only its
// author can see.
type Review struct {
	ID          int64      `json:"id"`
	User        *User      `json:"user"`
	State       string     `json:"state"`
	SubmittedAt *time.Time `json:"submitted_at"`
}

// CheckRun is one check run on one commit, from GET /repos/{owner}/{repo}/
// commits/{sha}/check-runs. Conclusion is set once Status is "completed".
type CheckRun struct {
	ID          int64      `json:"id"`
	HeadSHA     string     `json:"head_sha"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  *string    `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// checkStatusCompleted is the one check run status with a conclusion.
const checkStatusCompleted = "completed"

// checkRunsPage is one page of the check-runs list, which wraps its items.
type checkRunsPage struct {
	CheckRuns []CheckRun `json:"check_runs"`
}

// Snapshot is one read of a pull request and everything sillage reads
// about it.
type Snapshot struct {
	// Instance names the GitHub instance (see Config.Instance).
	Instance string
	// Repository is the base repository the pull request was read from.
	Repository  Repository
	PullRequest PullRequest
	// Timeline, Comments, ReviewComments and Reviews are in GitHub's
	// order, oldest first.
	Timeline []TimelineEvent
	Comments []Comment
	// ReviewComments are the inline comments on the diff.
	ReviewComments []Comment
	Reviews        []Review
	// SHAs are the commits whose check runs were asked for: those the
	// timeline names, in its order, then the head. Those GitHub refused
	// are in Refused.
	SHAs []string
	// UncheckedSHAs are older commits whose check runs were not read, past
	// maxCheckedCommits: their results are unknown, not absent.
	UncheckedSHAs []string
	// Checks holds every check run of every commit in SHAs, attempts
	// included: a later commit's results never replace an earlier one's.
	Checks []CheckRun
	// Refused are the sub-reads GitHub refused while the pull request
	// itself was readable: what they hold is unknown, not absent.
	Refused []Refusal
	// ObservedAt is when the last of these was read.
	ObservedAt time.Time
}

// The parts of a pull request's read that may be refused without failing
// it: Refusal.Resource.
const (
	ResourceCheckRuns      = "check runs"
	ResourceReviewComments = "review comments"
)

// Refusal is one sub-read of a pull request GitHub answered 403 Forbidden
// or 404 Not Found, as when a token may read pull requests but not checks.
type Refusal struct {
	// Resource is ResourceCheckRuns or ResourceReviewComments.
	Resource string
	// SHA is the commit whose check runs were refused; "" otherwise.
	SHA string
	// Path is the API path refused; Code its HTTP status code.
	Path string
	Code int
	// Remembered says the client did not send this request: it answered
	// the repository's check runs refused less than checkRunsRefusalTTL
	// ago, and Code is that answer's. False means GitHub just refused it.
	Remembered bool
}
