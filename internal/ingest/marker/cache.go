package marker

import (
	"crypto/sha256"
	"sync"
	"time"
)

// maxCachedVerdicts bounds the verdicts cached: the tokens of the pods
// running at once, each rotated at most hourly.
const maxCachedVerdicts = 4096

// tokenKey is a token's SHA-256: the cache never holds a token.
type tokenKey = [sha256.Size]byte

// verdict is a token's pod, valid until a time.
type verdict struct {
	id    identity
	until time.Time
}

// verdictCache holds the verdicts of authenticated tokens until they
// expire. Full, it caches nothing more until one does.
type verdictCache struct {
	mu      sync.Mutex
	limit   int
	entries map[tokenKey]verdict
}

func newVerdictCache(limit int) *verdictCache {
	return &verdictCache{limit: limit, entries: make(map[tokenKey]verdict)}
}

// get returns the pod of the token of key, unless its verdict expired at
// now.
func (c *verdictCache) get(key tokenKey, now time.Time) (identity, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	if !ok {
		return identity{}, false
	}
	if !now.Before(v.until) {
		delete(c.entries, key)
		return identity{}, false
	}
	return v.id, true
}

// put caches id for the token of key until until, unless that is not
// after now or the cache is full of verdicts still valid at now.
func (c *verdictCache) put(key tokenKey, id identity, until, now time.Time) {
	if !until.After(now) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.limit {
		for k, v := range c.entries {
			if !now.Before(v.until) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.limit {
			return
		}
	}
	c.entries[key] = verdict{id: id, until: until}
}
