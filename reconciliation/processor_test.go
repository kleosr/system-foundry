package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDuplicateWebhookAppliesOnce(t *testing.T) {
	store := NewMemoryStore()
	secret := []byte("top-secret")
	calls := 0
	processor := NewProcessor(store, func(context.Context, []byte) error {
		calls++
		return nil
	}, secret)
	payload := []byte(`{"id":1}`)
	sig := Sign(payload, secret)
	first := processor.Accept(context.Background(), "stripe", "evt-1", payload, sig)
	second := processor.Accept(context.Background(), "stripe", "evt-1", payload, sig)
	if !first.OK || first.Value != StatusPending || !second.OK || second.Value != StatusPending {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	applied, err := processor.Drain(context.Background(), 10)
	if err != nil || applied != 1 || calls != 1 {
		t.Fatalf("applied=%d calls=%d err=%v", applied, calls, err)
	}
	applied, err = processor.Drain(context.Background(), 10)
	if err != nil || applied != 0 || calls != 1 {
		t.Fatalf("applied=%d calls=%d err=%v", applied, calls, err)
	}
}

func TestBadSignatureDoesNotStore(t *testing.T) {
	store := NewMemoryStore()
	processor := NewProcessor(store, func(context.Context, []byte) error { return nil }, []byte("top-secret"))
	outcome := processor.Accept(context.Background(), "stripe", "evt-1", []byte(`{}`), "00")
	if outcome.OK || outcome.Error.Code != "FORBIDDEN" {
		t.Fatal(outcome.Error)
	}
	if _, ok := store.Get(context.Background(), "stripe", "evt-1"); ok {
		t.Fatal("row stored")
	}
}

func TestWebhookMovesToDLQAfterMaxAttempts(t *testing.T) {
	store := NewMemoryStore()
	secret := []byte("top-secret")
	processor := NewProcessor(store, func(context.Context, []byte) error {
		return errors.New("down")
	}, secret)
	processor.maxAttempts = 3
	now := time.Unix(1_700_000_000, 0)
	processor.now = func() time.Time { return now }
	payload := []byte(`{"id":1}`)
	outcome := processor.Accept(context.Background(), "stripe", "evt-1", payload, Sign(payload, secret))
	if !outcome.OK {
		t.Fatal(outcome.Error)
	}
	for i := 0; i < 3; i++ {
		now = now.Add(time.Minute)
		if _, err := processor.Drain(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
	}
	row, ok := store.Get(context.Background(), "stripe", "evt-1")
	if !ok || row.Status != StatusDead {
		t.Fatalf("row = %+v", row)
	}
}

func TestReplayRunsDeadEventOnce(t *testing.T) {
	store := NewMemoryStore()
	secret := []byte("top-secret")
	calls := 0
	fail := true
	processor := NewProcessor(store, func(context.Context, []byte) error {
		calls++
		if fail {
			return errors.New("down")
		}
		return nil
	}, secret)
	processor.maxAttempts = 1
	payload := []byte(`{"id":1}`)
	if outcome := processor.Accept(context.Background(), "stripe", "evt-1", payload, Sign(payload, secret)); !outcome.OK {
		t.Fatal(outcome.Error)
	}
	if _, err := processor.Drain(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	replay := processor.Replay(context.Background(), "stripe", "evt-1")
	if !replay.OK || replay.Value != StatusPending {
		t.Fatal(replay.Error)
	}
	fail = false
	applied, err := processor.Drain(context.Background(), 10)
	if err != nil || applied != 1 || calls != 2 {
		t.Fatalf("applied=%d calls=%d err=%v", applied, calls, err)
	}
}
