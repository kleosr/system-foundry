package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryBucketRefillsOnWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := NewMemoryStore(func() time.Time { return now })
	window := time.Second
	first, err := store.Take(context.Background(), "ratelimit:tenant:acme:v1", 1, window)
	if err != nil || !first {
		t.Fatalf("first = %v %v", first, err)
	}
	second, err := store.Take(context.Background(), "ratelimit:tenant:acme:v1", 1, window)
	if err != nil || second {
		t.Fatalf("second = %v %v", second, err)
	}
	now = now.Add(window)
	third, err := store.Take(context.Background(), "ratelimit:tenant:acme:v1", 1, window)
	if err != nil || !third {
		t.Fatalf("third = %v %v", third, err)
	}
}

func TestLimiterDeniesOverSharedLimit(t *testing.T) {
	store := NewMemoryStore(time.Now)
	limiter, err := NewLimiter(store, 2, 5, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		decision, allowErr := limiter.Allow(context.Background(), "acme")
		if allowErr != nil || !decision.Allowed || decision.Degraded {
			t.Fatalf("decision = %+v %v", decision, allowErr)
		}
	}
	decision, err := limiter.Allow(context.Background(), "acme")
	if err != nil || decision.Allowed || decision.Degraded {
		t.Fatalf("decision = %+v %v", decision, err)
	}
}

func TestLimiterDegradesWhenStoreFails(t *testing.T) {
	limiter, err := NewLimiter(downStore{}, 100, 2, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		decision, allowErr := limiter.Allow(context.Background(), "acme")
		if allowErr != nil || !decision.Allowed || !decision.Degraded {
			t.Fatalf("decision = %+v %v", decision, allowErr)
		}
	}
	decision, err := limiter.Allow(context.Background(), "acme")
	if err != nil || decision.Allowed || !decision.Degraded {
		t.Fatalf("decision = %+v %v", decision, err)
	}
}

func TestLimiterRejectsBadTenant(t *testing.T) {
	limiter, err := NewLimiter(NewMemoryStore(time.Now), 10, 5, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = limiter.Allow(context.Background(), "bad tenant")
	if !errors.Is(err, errTenant) {
		t.Fatal(err)
	}
}

type downStore struct{}

func (downStore) Take(context.Context, string, int, time.Duration) (bool, error) {
	return false, errors.New("partition")
}
