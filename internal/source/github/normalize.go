package github

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelostext"
)

const (
	// botType is User.Type for a GitHub App or other bot account.
	botType = "Bot"
	// reviewPending is the state of a review not yet submitted.
	reviewPending = "PENDING"
)

// Normalize turns one read of a pull request into model records: the pull
// request with its check runs, and its events (opened, closed, merged,
// reopened, review requests, conversation and inline review comments and
// requests, start announcements posted by one of announcers, submitted
// reviews), in time order. Each event's actor is the GitHub account; its
// executor stays unset unless executorOf or a start announcement names it. A merge is a pr.merged
// event and the close that comes with it is dropped: pr.closed means
// closed without being merged. Neither is the task being done. Every
// record is validated.
func Normalize(s Snapshot, announcers []string) (model.Records, error) {
	n := normalizer{s: s, key: prKey(s), announcers: announcers, seen: make(map[string]bool)}
	n.add(n.opened())
	if err := n.timeline(); err != nil {
		return model.Records{}, err
	}
	for _, c := range s.Comments {
		n.add(n.conversationComment(c))
	}
	n.reviewComments()
	for _, r := range s.Reviews {
		if submitted(r) {
			n.add(n.review(r))
		}
	}
	slices.SortStableFunc(n.events, func(a, b model.Event) int { return a.Time().Compare(b.Time()) })
	pr, err := n.pullRequest()
	if err != nil {
		return model.Records{}, err
	}
	if err := validate(n.events, pr); err != nil {
		return model.Records{}, err
	}
	return model.Records{Events: n.events, PullRequests: []model.PullRequest{pr}}, nil
}

// normalizer accumulates the events of one snapshot, each id once: a
// record repeated across two pages read while it changed counts once.
type normalizer struct {
	s          Snapshot
	key        model.PRKey
	announcers []string
	events     []model.Event
	seen       map[string]bool
}

func (n *normalizer) add(e model.Event) {
	if !n.seen[e.ID] {
		n.seen[e.ID] = true
		n.events = append(n.events, e)
	}
}

// prKey names the pull request by its base repository as GitHub spells it.
func prKey(s Snapshot) model.PRKey {
	repo := s.Repository.String()
	if r := s.PullRequest.Base.Repo; r != nil && r.FullName != "" {
		repo = r.FullName
	}
	return model.PRKey{Instance: s.Instance, Repository: repo, Number: s.PullRequest.Number}
}

// event fills the fields every event of the snapshot shares.
func (n *normalizer) event(kind model.EventKind, id string, at time.Time, actor *User, ref, note string) model.Event {
	key := n.key
	return model.Event{
		Source:     model.SourceGitHub,
		Instance:   n.s.Instance,
		ID:         id,
		IDKind:     model.IDSource,
		Kind:       kind,
		SourceTime: &at,
		ObservedAt: n.s.ObservedAt,
		Actor:      account(actor),
		Related:    model.Related{PR: &key},
		Evidence: model.Evidence{
			Source:   model.SourceGitHub,
			Instance: n.s.Instance,
			Ref:      "GET " + ref,
			Note:     note,
		},
	}
}

// account is the account GitHub names: a bot account is a service, any
// other a person, since an agent posts as its owner's user. A deleted
// account is unknown.
func account(u *User) model.Account {
	switch {
	case u == nil || u.Login == "":
		return model.Account{Kind: model.ActorUnknown}
	case u.Type == botType:
		return model.Account{Login: u.Login, Kind: model.ActorService}
	}
	return model.Account{Login: u.Login, Kind: model.ActorPerson}
}

func (n *normalizer) opened() model.Event {
	pr := n.s.PullRequest
	note := "created_at, user"
	if u := pr.User; u != nil {
		note = "user " + u.Login
	}
	e := n.event(model.KindPROpened, "pulls/"+strconv.FormatInt(pr.ID, 10), pr.CreatedAt, pr.User,
		pullPath(n.s.Repository, pr.Number), note)
	e.Details = map[string]string{"branch": pr.Head.Ref}
	if ex, ok := executorOf(pr, n.s.Timeline); ok {
		e.Executor = &model.Executor{Name: ex.agent, Kind: model.ActorAgent}
		e.Evidence.Note += "; label " + ex.label + ", added by the author at creation (event " +
			strconv.FormatInt(ex.eventID, 10) + "), names the executor"
	}
	return e
}

// comment is a comment, or a request when it asks an agent to act. Its id
// is idPrefix and GitHub's id, unique among comments of its endpoint path.
func (n *normalizer) comment(c Comment, idPrefix, path, notePrefix string) model.Event {
	id := strconv.FormatInt(c.ID, 10)
	e := n.event(model.KindComment, idPrefix+id, c.CreatedAt, c.User, path, notePrefix+id)
	if r, ok := kelostext.ParseRequest(c.Body); ok {
		e.Kind = model.KindRequest
		setDetail(&e, "command", r.Command)
		if r.Agent != "" {
			setDetail(&e, "agent", r.Agent)
		}
	}
	return e
}

// conversationComment is a conversation comment: a request, a start
// announcement when one of the announcers posted it, or a plain comment.
// Kelos announces runs only there.
func (n *normalizer) conversationComment(c Comment) model.Event {
	e := n.comment(c, commentIDPrefix, commentsPath(n.s.Repository, n.s.PullRequest.Number), "comment ")
	if e.Kind != model.KindComment || !n.announcer(c.User) {
		return e
	}
	a, err := parseAnnouncement(c.Body, n.s.PullRequest.Number)
	switch {
	case err == nil:
		announced(&e, a)
	case errors.Is(err, kelostext.ErrUnparsedAnnouncement):
		e.Evidence.Note += "; reads like a start announcement but names no Kelos Task of this pull request"
	}
	return e
}

// announcer reports whether u is one of the announcers. GitHub logins are
// case-insensitive.
func (n *normalizer) announcer(u *User) bool {
	return u != nil && slices.ContainsFunc(n.announcers, func(l string) bool { return strings.EqualFold(l, u.Login) })
}

// reviewComments adds the inline review comments others can see. One
// drafted in a review still pending is skipped. One posted with a review
// is dated no earlier than the review's submission: its own created_at is
// when it was drafted, and nobody saw it before the review was submitted.
func (n *normalizer) reviewComments() {
	reviews := make(map[int64]Review, len(n.s.Reviews))
	for _, r := range n.s.Reviews {
		reviews[r.ID] = r
	}
	path := reviewCommentsPath(n.s.Repository, n.s.PullRequest.Number)
	for _, c := range n.s.ReviewComments {
		r, inReview := reviews[c.ReviewID]
		if inReview && !submitted(r) {
			continue
		}
		later := inReview && r.SubmittedAt.After(c.CreatedAt)
		if later {
			c.CreatedAt = *r.SubmittedAt
		}
		e := n.comment(c, reviewCommentIDPrefix, path, "review comment ")
		if later {
			e.Evidence.Note += "; time of review " + strconv.FormatInt(r.ID, 10) + "'s submission, not of its draft"
		}
		n.add(e)
	}
}

// submitted reports whether a review was submitted, so others can see it.
func submitted(r Review) bool {
	return r.SubmittedAt != nil && r.State != reviewPending
}

func (n *normalizer) review(r Review) model.Event {
	id := strconv.FormatInt(r.ID, 10)
	e := n.event(model.KindReviewSubmitted, reviewIDPrefix+id, *r.SubmittedAt, r.User,
		pullPath(n.s.Repository, n.s.PullRequest.Number)+"/reviews", "review "+id)
	e.Details = map[string]string{"state": strings.ToLower(r.State)}
	return e
}

func validate(events []model.Event, pr model.PullRequest) error {
	errs := []error{pr.Validate()}
	for _, e := range events {
		errs = append(errs, e.Validate())
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("github: pull request %s: %w", pr.Key, err)
	}
	return nil
}

// pullRequest is the pull request with every check run read, each once,
// whatever its commit.
func (n *normalizer) pullRequest() (model.PullRequest, error) {
	pr := model.PullRequest{Key: n.key, Branch: n.s.PullRequest.Head.Ref, Checks: []model.CheckRun{}}
	seen := make(map[int64]bool)
	for _, c := range n.s.Checks {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		check, err := checkRun(c)
		if err != nil {
			return model.PullRequest{}, fmt.Errorf("github: pull request %s: check run %d: %w", n.key, c.ID, err)
		}
		pr.Checks = append(pr.Checks, check)
	}
	slices.SortStableFunc(pr.Checks, func(a, b model.CheckRun) int { return cmp.Compare(a.ID, b.ID) })
	return pr, nil
}

// checkRun converts a check run: a run not completed is pending with an
// open end; a time GitHub does not give is an unknown bound.
func checkRun(c CheckRun) (model.CheckRun, error) {
	out := model.CheckRun{
		ID:   c.ID,
		SHA:  c.HeadSHA,
		Name: c.Name,
		Interval: model.Interval{
			Start: boundAt(c.StartedAt),
			End:   model.OpenEnd(),
		},
		Conclusion: model.CheckPending,
	}
	if c.Status != checkStatusCompleted {
		return out, nil
	}
	if c.Conclusion == nil {
		return model.CheckRun{}, errors.New("completed without a conclusion")
	}
	out.Conclusion = model.CheckConclusion(*c.Conclusion)
	out.Interval.End = boundAt(c.CompletedAt)
	return out, nil
}

func boundAt(t *time.Time) model.Bound {
	if t == nil {
		return model.UnknownBound()
	}
	return model.KnownAt(*t)
}
