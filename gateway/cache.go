package gateway

import (
	"sync"
	"time"

	"systemsfoundry/platform"
)

type cacheEntry struct {
	tenantID   string
	vector     []float32
	completion Completion
	expires    time.Time
}

type Cache struct {
	mu      sync.Mutex
	max     int
	entries []cacheEntry
}

func NewCache(max int) *Cache {
	if max < 1 {
		max = 10000
	}
	return &Cache{max: max}
}

func (c *Cache) Find(tenantID string, vector []float32, threshold float64, now time.Time) (Completion, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropExpired(now)
	var best float64
	var found Completion
	ok := false
	for _, entry := range c.entries {
		if entry.tenantID != tenantID {
			continue
		}
		score := platform.Cosine(entry.vector, vector)
		if score >= threshold && (!ok || score > best) {
			best = score
			found = entry.completion
			ok = true
		}
	}
	return found, ok
}

func (c *Cache) Save(tenantID string, vector []float32, completion Completion, expires time.Time, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropExpired(now)
	if len(c.entries) >= c.max {
		c.entries = c.entries[1:]
	}
	c.entries = append(c.entries, cacheEntry{
		tenantID:   tenantID,
		vector:     append([]float32(nil), vector...),
		completion: completion,
		expires:    expires,
	})
}

func (c *Cache) dropExpired(now time.Time) {
	kept := c.entries[:0]
	for _, entry := range c.entries {
		if entry.expires.After(now) {
			kept = append(kept, entry)
		}
	}
	c.entries = kept
}
