package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGateRejectsWhenFull(t *testing.T) {
	gate, err := NewGate(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = gate.Enter(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = gate.Enter(context.Background()); !errors.Is(err, ErrSaturated) {
		t.Fatalf("err = %v", err)
	}
	gate.Leave()
	if err = gate.Enter(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGateWaitsThenRejectsCancelledContext(t *testing.T) {
	gate, err := NewGate(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = gate.Enter(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = gate.Enter(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("enter returned before the wait")
	}
	gate.Leave()
	if err = gate.Enter(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewGateRejectsZeroFlight(t *testing.T) {
	if _, err := NewGate(0, 1); err == nil {
		t.Fatal("expected error")
	}
}
