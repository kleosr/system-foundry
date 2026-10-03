package auth

import (
	"sync"
	"time"
)

type Throttle struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	failures map[string][]time.Time
}

func NewThrottle(limit int, window time.Duration) *Throttle {
	if limit < 1 {
		limit = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Throttle{limit: limit, window: window, failures: map[string][]time.Time{}}
}

func (t *Throttle) Allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.prune(key, now)) < t.limit
}

func (t *Throttle) Fail(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept := t.prune(key, now)
	t.failures[key] = append(kept, now)
}

func (t *Throttle) Clear(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}

func (t *Throttle) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-t.window)
	kept := t.failures[key][:0]
	for _, at := range t.failures[key] {
		if !at.Before(cutoff) {
			kept = append(kept, at)
		}
	}
	t.failures[key] = kept
	return kept
}
