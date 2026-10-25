package jev

import (
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
)

const cacheTTL = 10 * time.Minute
const cacheSize = 256

type cacheEntry struct {
	created    time.Time
	candidates []discovery.Candidate
}

func clone(candidates []discovery.Candidate) []discovery.Candidate {
	out := append([]discovery.Candidate(nil), candidates...)
	for i := range out {
		out[i].Evidence = append([]byte(nil), out[i].Evidence...)
	}
	return out
}

func (c *Client) cached(key [32]byte) ([]discovery.Candidate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		c.metrics.Cache("jev", false)
		return nil, false
	}
	if time.Since(entry.created) >= cacheTTL {
		c.metrics.Cache("jev", false)
		delete(c.cache, key)
		return nil, false
	}
	c.metrics.Cache("jev", true)
	return clone(entry.candidates), true
}

func (c *Client) remember(key [32]byte, candidates []discovery.Candidate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= cacheSize {
		var oldest [32]byte
		var created time.Time
		for k, entry := range c.cache {
			if created.IsZero() || entry.created.Before(created) {
				oldest, created = k, entry.created
			}
		}
		delete(c.cache, oldest)
	}
	c.cache[key] = cacheEntry{created: time.Now(), candidates: clone(candidates)}
}
