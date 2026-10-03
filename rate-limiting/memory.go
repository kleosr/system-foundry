package ratelimit

import (
	"context"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	seen   time.Time
}

type MemoryStore struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets map[string]*bucket
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{now: now, buckets: map[string]*bucket{}}
}

func (s *MemoryStore) Take(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	if limit < 1 || window <= 0 {
		return false, errBadLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	current := s.buckets[key]
	if current == nil || now.Sub(current.seen) >= window*2 {
		current = &bucket{tokens: float64(limit), seen: now}
		s.buckets[key] = current
	}
	elapsed := now.Sub(current.seen)
	if elapsed > 0 {
		current.tokens += float64(elapsed) / float64(window) * float64(limit)
		if current.tokens > float64(limit) {
			current.tokens = float64(limit)
		}
		current.seen = now
	}
	if current.tokens < 1 {
		return false, nil
	}
	current.tokens--
	return true, nil
}
