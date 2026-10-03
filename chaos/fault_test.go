package chaos

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLatencyHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	called := false
	err := Apply(ctx, Plan{Latency: time.Second}, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if called {
		t.Fatal("next ran after cancel")
	}
}

func TestPartitionSkipsNext(t *testing.T) {
	called := false
	err := Apply(context.Background(), Plan{Partition: true}, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrPartition) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestKillSkipsNext(t *testing.T) {
	called := false
	err := Apply(context.Background(), Plan{Kill: true}, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrReplicaKilled) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestClearPlanRunsNext(t *testing.T) {
	called := false
	err := Apply(context.Background(), Plan{}, func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestBurnRatePagesOnFastErrorBudgetSpend(t *testing.T) {
	objective := 0.999
	shortRate := BurnRate(Sample{Bad: 20, Total: 1000}, objective)
	longRate := BurnRate(Sample{Bad: 2, Total: 1000}, objective)
	if shortRate <= 14.4 {
		t.Fatalf("short = %v", shortRate)
	}
	if longRate <= 1 {
		t.Fatalf("long = %v", longRate)
	}
	if !ShouldPage(shortRate, longRate) {
		t.Fatal("expected page")
	}
	quiet := BurnRate(Sample{Bad: 0, Total: 1000}, objective)
	if quiet != 0 || ShouldPage(quiet, quiet) {
		t.Fatal("quiet window paged")
	}
}
