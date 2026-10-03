package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

func sample(id string) OrderEvent {
	return OrderEvent{
		ID:       id,
		TenantID: "acme",
		OrderID:  "order-1",
		Kind:     KindPlaced,
		Occurred: time.Unix(1_700_000_000, 0).UTC(),
		Payload:  []byte(`{"sku":"shoe"}`),
	}
}

func TestDuplicatePublishIsRejected(t *testing.T) {
	log := NewMemoryLog(3)
	if err := log.Append(sample("evt-1")); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(sample("evt-1")); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
}

func TestEffectRunsOnceWhenRedelivered(t *testing.T) {
	log := NewMemoryLog(5)
	calls := 0
	ready := false
	pipeline := NewPipeline(log, func(context.Context, OrderEvent) error {
		calls++
		if !ready {
			return errors.New("not ready")
		}
		return nil
	})
	if err := pipeline.Publish(sample("evt-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Drain(context.Background(), 1); err == nil {
		t.Fatal("expected apply failure")
	}
	ready = true
	if _, err := pipeline.Drain(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Drain(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestSuccessfulEffectIsNotRepeated(t *testing.T) {
	log := NewMemoryLog(5)
	calls := 0
	pipeline := NewPipeline(log, func(context.Context, OrderEvent) error {
		calls++
		return nil
	})
	if err := pipeline.Publish(sample("evt-1")); err != nil {
		t.Fatal(err)
	}
	event, ok := log.Pull()
	if !ok {
		t.Fatal("missing event")
	}
	if err := pipeline.Deliver(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Deliver(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestNakMovesEventToDeadLetter(t *testing.T) {
	log := NewMemoryLog(2)
	if err := log.Append(sample("evt-1")); err != nil {
		t.Fatal(err)
	}
	if !log.Nak("evt-1") || !log.Nak("evt-1") {
		t.Fatal("nak failed")
	}
	if _, ok := log.Pull(); ok {
		t.Fatal("dead event was pulled")
	}
	dead := log.Dead()
	if len(dead) != 1 || dead[0].ID != "evt-1" {
		t.Fatalf("dead = %+v", dead)
	}
}

func TestPayloadMutationDoesNotChangeLog(t *testing.T) {
	log := NewMemoryLog(2)
	event := sample("evt-1")
	if err := log.Append(event); err != nil {
		t.Fatal(err)
	}
	event.Payload[0] = 'X'
	pulled, ok := log.Pull()
	if !ok || string(pulled.Payload) != `{"sku":"shoe"}` {
		t.Fatalf("payload = %s", pulled.Payload)
	}
}
