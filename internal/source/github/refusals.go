package github

import (
	"strings"
	"time"
)

// checkRunsRefusal is a refusal of a repository's check runs and when it
// stops being remembered.
type checkRunsRefusal struct {
	code  int
	until time.Time
}

// refusalKey is the case-insensitive key of repo: GitHub's owner and
// repository names are.
func refusalKey(repo Repository) string { return strings.ToLower(repo.String()) }

// rememberedCheckRunsRefusal returns the status GitHub refused repo's
// check runs with, and true, when that was less than checkRunsRefusalTTL
// ago.
func (c *Client) rememberedCheckRunsRefusal(repo Repository) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := refusalKey(repo)
	r, ok := c.checkRunsRefused[key]
	if !ok {
		return 0, false
	}
	if !c.now().Before(r.until) {
		delete(c.checkRunsRefused, key)
		return 0, false
	}
	return r.code, true
}

// rememberCheckRunsRefusal remembers that GitHub refused repo's check runs
// with code, for checkRunsRefusalTTL.
func (c *Client) rememberCheckRunsRefusal(repo Repository, code int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checkRunsRefused == nil {
		c.checkRunsRefused = make(map[string]checkRunsRefusal)
	}
	c.checkRunsRefused[refusalKey(repo)] = checkRunsRefusal{code: code, until: c.now().Add(checkRunsRefusalTTL)}
}
