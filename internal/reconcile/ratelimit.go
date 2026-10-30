package reconcile

import (
	"errors"
	"time"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// What the loop leaves of GitHub's rate limit, and how it waits one out.
const (
	// githubReserve is the share of GitHub's rate limit the backstop
	// leaves to the reads deliveries, runs and task changes ask for, and
	// to the other clients of the same token: below it, no backstop read
	// is made.
	githubReserve = 0.2
	// githubTriggerReserve is the share the reads deliveries, runs and
	// task changes ask for leave to the token's other clients: below it,
	// they are deferred to a later cycle, the longest deferred read first
	// once the window resets, so that a catch-up is spread over several
	// windows instead of exhausting one.
	githubTriggerReserve = 0.1
	// githubRateWindow is how long GitHub's rate-limit window lasts: the
	// retries of the roots a rate limit failed are spread over the window
	// that follows its reset.
	githubRateWindow = time.Hour
	// rateLimitPause is how long GitHub is left alone after a rate limit
	// that names no wait: the least GitHub asks for after a secondary rate
	// limit.
	rateLimitPause = time.Minute
)

// owedGitHub is a read of a root's GitHub side that a delivery, a run or
// a task change asked for and the rate limit deferred: every cycle reads
// it, until one can afford it.
type owedGitHub struct {
	// Trigger is what asked for the read first, and Since when it was
	// first deferred.
	Trigger string    `json:"trigger"`
	Since   time.Time `json:"since"`
}

// owe records that the GitHub read trigger asked for was deferred at at,
// unless one is owed since earlier.
func (s *rootState) owe(trigger string, at time.Time) {
	if s.GitHubOwed == nil {
		s.GitHubOwed = &owedGitHub{Trigger: trigger, Since: at}
	}
}

// githubAllowed reports whether a read may ask GitHub anything: no rate
// limit is being waited out, and the rate limit's window leaves more than
// reserve of it. An unknown window, or one past its reset, allows.
func (l *Loop) githubAllowed(reserve float64) bool {
	if l.githubPaused() {
		return false
	}
	if l.conditional == nil {
		return true
	}
	b := l.conditional.Usage().Budget
	if b.Limit <= 0 || !l.now().Before(b.Reset) {
		return true
	}
	return float64(b.Remaining) > reserve*float64(b.Limit)
}

// githubPaused reports whether GitHub refused a request for its rate
// limit and the wait it asked for is not over: no GitHub request is made
// until it is.
func (l *Loop) githubPaused() bool { return l.now().Before(l.githubResume) }

// noteGitHubError pauses every GitHub request when err is a rate limit,
// until the wait GitHub asked for is over, and says so when the pause
// starts. It reports whether err was one.
func (l *Loop) noteGitHubError(err error) bool {
	if reasonOf(err) != reasonRateLimited {
		return false
	}
	resume := l.now().Add(rateLimitPause)
	if e, ok := errors.AsType[*github.RateLimitError](err); ok && e.Wait > 0 {
		resume = l.now().Add(e.Wait)
	}
	if !l.githubPaused() {
		l.log.Warn("GitHub rate limited: no GitHub request until the wait it asked for is over",
			"source", model.SourceGitHub, "instance", l.ccfg.GitHubInstance, "resume", resume, "error", err)
	}
	if resume.After(l.githubResume) {
		l.githubResume = resume
	}
	return true
}

// retryAfterRateLimit schedules the backstop of st's root, whose GitHub
// read the rate limit failed, within the window that follows the pause:
// the roots one rate limit failed do not all retry at once.
func (l *Loop) retryAfterRateLimit(st *rootState) {
	st.backstop = l.githubResume.Add(l.spread(githubRateWindow))
}

// addOwed adds to dues the GitHub read each root is owed, since when: a
// root due for nothing else is due for that alone. With the GitHub source
// off, an owed read waits for it to come back: no read could clear it.
func (l *Loop) addOwed(dues map[model.Ref]*due) {
	if l.src.GitHub == nil {
		return
	}
	for ref, st := range l.roots {
		if st.GitHubOwed == nil {
			continue
		}
		d, ok := dues[ref]
		if !ok {
			d = &due{root: st.Root, githubOnly: true}
			dues[ref] = d
		}
		if d.github != githubFull {
			only := d.githubOnly
			d.readGitHub(st.GitHubOwed.Trigger)
			d.githubOnly = only
		}
		since := st.GitHubOwed.Since
		d.owed = &since
	}
}
