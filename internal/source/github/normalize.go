package github

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sperano/sillage/internal/model"
)

const (
	// requestPrefix starts a comment asking an agent to act.
	requestPrefix = "/kelos"
	// botType is User.Type for a GitHub App or other bot account.
	botType = "Bot"
	// reviewPending is the state of a review not yet submitted.
	reviewPending = "PENDING"
)

// Normalize turns one read of a pull request into model records: the pull
// request with its check runs, and its events (opened, closed, merged,
// reopened, review requests, comments and requests, submitted reviews), in
// time order. Each event's actor is the GitHub account; its executor stays
// unset unless executorOf finds evidence. A merge is a pr.merged event and
// the close that comes with it is dropped: pr.closed means closed without
// being merged. Neither is the task being done. Every record is validated.
func Normalize(s Snapshot) (model.Records, error) {
	n := normalizer{s: s, key: prKey(s), seen: make(map[string]bool)}
	n.add(n.opened())
	if err := n.timeline(); err != nil {
		return model.Records{}, err
	}
	for _, c := range s.Comments {
		n.add(n.comment(c))
	}
	for _, r := range s.Reviews {
		if r.SubmittedAt != nil && r.State != reviewPending {
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
	s      Snapshot
	key    model.PRKey
	events []model.Event
	seen   map[string]bool
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

func (n *normalizer) comment(c IssueComment) model.Event {
	id := strconv.FormatInt(c.ID, 10)
	kind := model.KindComment
	if isRequest(c.Body) {
		kind = model.KindRequest
	}
	return n.event(kind, "issues/comments/"+id, c.CreatedAt, c.User,
		commentsPath(n.s.Repository, n.s.PullRequest.Number), "comment "+id)
}

// isRequest reports whether a comment body starts with requestPrefix as a
// word.
func isRequest(body string) bool {
	rest, ok := strings.CutPrefix(strings.TrimLeftFunc(body, unicode.IsSpace), requestPrefix)
	if !ok {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return rest == "" || unicode.IsSpace(r)
}

func (n *normalizer) review(r Review) model.Event {
	id := strconv.FormatInt(r.ID, 10)
	e := n.event(model.KindReviewSubmitted, "reviews/"+id, *r.SubmittedAt, r.User,
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
