package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisBucketRefillsAndSetsTTL(t *testing.T) {
	server := miniredis.RunT(t)
	store := NewRedisStore(server.Addr(), time.Second)
	now := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return now }
	key, ok := Key("acme")
	if !ok {
		t.Fatal("key")
	}
	first, err := store.Take(context.Background(), key, 1, time.Second)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	second, err := store.Take(context.Background(), key, 1, time.Second)
	if err != nil || second {
		t.Fatalf("second=%v err=%v", second, err)
	}
	ttl := server.TTL(key)
	if ttl <= 0 {
		t.Fatalf("ttl = %s", ttl)
	}
	now = now.Add(time.Second)
	third, err := store.Take(context.Background(), key, 1, time.Second)
	if err != nil || !third {
		t.Fatalf("third=%v err=%v", third, err)
	}
}
