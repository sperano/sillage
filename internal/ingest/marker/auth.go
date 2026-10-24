package marker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	// Audience is the audience of the projected tokens sillage accepts:
	// a token for the API server, or for anything else, is refused.
	Audience = "sillage"
	// maxTokenBytes bounds a bearer token sent for review: a projected
	// ServiceAccount token is about a kilobyte.
	maxTokenBytes = 8 << 10
	// maxVerdictLifetime bounds how long a verdict is cached, whatever the
	// token's expiry: the pods' tokens are projected for an hour.
	maxVerdictLifetime = time.Hour
	// reviewTimeout bounds one TokenReview.
	reviewTimeout = 5 * time.Second
	// maxReviewsInFlight bounds the TokenReviews running at once, so that
	// a flood of tokens never cached costs the API server little.
	maxReviewsInFlight = 4
	// reviewSlotWait is how long a request waits for a review slot.
	reviewSlotWait = time.Second
	// serviceAccountPrefix starts a ServiceAccount's user name:
	// system:serviceaccount:<namespace>:<name>.
	serviceAccountPrefix = "system:serviceaccount:"
	// extraPodName and extraPodUID are the TokenReview's extra fields
	// naming the pod a bound token was issued for.
	extraPodName = "authentication.kubernetes.io/pod-name"
	extraPodUID  = "authentication.kubernetes.io/pod-uid"
	bearerScheme = "bearer"
	jwtParts     = 3
)

// ReviewStatus is a TokenReview's status: what the API server says of a
// token.
type ReviewStatus struct {
	Authenticated bool     `json:"authenticated"`
	User          UserInfo `json:"user"`
	// Audiences are the token's audiences among those the review asked
	// for.
	Audiences []string `json:"audiences,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// UserInfo is the user a token authenticates.
type UserInfo struct {
	Username string              `json:"username"`
	UID      string              `json:"uid,omitempty"`
	Groups   []string            `json:"groups,omitempty"`
	Extra    map[string][]string `json:"extra,omitempty"`
}

// Reviewer reviews a token for Audience: a TokenReview.
type Reviewer interface {
	// Review returns what the API server says of token; an error when it
	// could not be asked.
	Review(ctx context.Context, token string) (ReviewStatus, error)
}

// identity is what a review established of a token's pod.
type identity struct {
	namespace, serviceAccount, pod, podUID string
}

// refusal is a request refused before its marker is recorded, with the
// status that answers it.
type refusal struct {
	status int
	err    error
	// public, when set, answers the request instead of err, which only
	// the log holds: what the API server said is not the sender's to read.
	public string
}

func (e *refusal) Error() string { return e.err.Error() }

// answer is what the request is answered.
func (e *refusal) answer() string {
	if e.public != "" {
		return e.public
	}
	return e.err.Error()
}

func (e *refusal) Unwrap() error { return e.err }

func unauthorized(format string, args ...any) *refusal {
	return &refusal{status: http.StatusUnauthorized, err: fmt.Errorf(format, args...)}
}

var errBusy = errors.New("too many token reviews are running: try again")

// reviewFailed answers a request whose token review failed.
const reviewFailed = "the token could not be reviewed"

// authenticator authenticates requests by their bearer token.
type authenticator struct {
	reviewer  Reviewer
	namespace string
	now       func() time.Time
	cache     *verdictCache
	slots     chan struct{}
}

func newAuthenticator(r Reviewer, namespace string, now func() time.Time) *authenticator {
	return &authenticator{reviewer: r, namespace: namespace, now: now,
		cache: newVerdictCache(maxCachedVerdicts), slots: make(chan struct{}, maxReviewsInFlight)}
}

// authenticate returns the pod the request's token was issued for.
func (a *authenticator) authenticate(ctx context.Context, header string) (identity, error) {
	token, err := bearer(header)
	if err != nil {
		return identity{}, err
	}
	key := sha256.Sum256([]byte(token))
	if id, ok := a.cache.get(key, a.now()); ok {
		return id, nil
	}
	st, err := a.review(ctx, token)
	if err != nil {
		return identity{}, err
	}
	id, err := a.identify(st)
	if err != nil {
		return identity{}, err
	}
	if exp, ok := tokenExpiry(token); ok {
		now := a.now()
		until := now.Add(maxVerdictLifetime)
		if exp.Before(until) {
			until = exp
		}
		a.cache.put(key, id, until, now)
	}
	return id, nil
}

// review asks the reviewer about token, once a slot is free.
func (a *authenticator) review(ctx context.Context, token string) (ReviewStatus, error) {
	wait := time.NewTimer(reviewSlotWait)
	defer wait.Stop()
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	case <-wait.C:
		return ReviewStatus{}, &refusal{status: http.StatusServiceUnavailable, err: errBusy}
	case <-ctx.Done():
		return ReviewStatus{}, &refusal{status: http.StatusServiceUnavailable, err: errBusy}
	}
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	st, err := a.reviewer.Review(ctx, token)
	if err != nil {
		return ReviewStatus{}, &refusal{status: http.StatusServiceUnavailable,
			err: fmt.Errorf("the token review failed: %w", err), public: reviewFailed}
	}
	return st, nil
}

// identify checks that st authenticates a ServiceAccount of the Kelos
// namespace for Audience, bound to one pod, and returns it.
func (a *authenticator) identify(st ReviewStatus) (identity, error) {
	if !st.Authenticated {
		return identity{}, unauthorized("the token is not authenticated")
	}
	if !slices.Contains(st.Audiences, Audience) {
		return identity{}, unauthorized("the token is not for audience %q", Audience)
	}
	ns, sa, ok := strings.Cut(strings.TrimPrefix(st.User.Username, serviceAccountPrefix), ":")
	if !strings.HasPrefix(st.User.Username, serviceAccountPrefix) || !ok || sa == "" {
		return identity{}, unauthorized("the token is no ServiceAccount's")
	}
	if ns != a.namespace {
		return identity{}, unauthorized("ServiceAccount %s:%s is not of namespace %s", ns, sa, a.namespace)
	}
	pod, podOK := only(st.User.Extra[extraPodName])
	uid, uidOK := only(st.User.Extra[extraPodUID])
	if !podOK || !uidOK {
		return identity{}, unauthorized("the token is bound to no pod")
	}
	return identity{namespace: ns, serviceAccount: sa, pod: pod, podUID: uid}, nil
}

// only returns the one value of values, if it has exactly one, not empty.
func only(values []string) (string, bool) {
	if len(values) != 1 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

// bearer returns the token of an Authorization header.
func bearer(header string) (string, error) {
	scheme, token, ok := strings.Cut(header, " ")
	switch {
	case !ok || !strings.EqualFold(scheme, bearerScheme) || token == "" || strings.ContainsAny(token, " \t"):
		return "", unauthorized("a bearer token is required")
	case len(token) > maxTokenBytes:
		return "", unauthorized("the token is longer than %d bytes", maxTokenBytes)
	}
	return token, nil
}

// tokenExpiry reads the expiry a JWT claims. It is trusted only once a
// review authenticated the token, which the API server signed.
func tokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != jwtParts {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp *int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == nil {
		return time.Time{}, false
	}
	return time.Unix(*claims.Exp, 0), true
}
