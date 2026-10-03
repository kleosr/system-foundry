package chaos

import (
	"context"
	"errors"
	"time"
)

var ErrReplicaKilled = errors.New("replica killed")
var ErrPartition = errors.New("network partition")

type Plan struct {
	Latency   time.Duration
	Kill      bool
	Partition bool
}

func Apply(ctx context.Context, plan Plan, next func(context.Context) error) error {
	if plan.Latency > 0 {
		timer := time.NewTimer(plan.Latency)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if plan.Partition {
		return ErrPartition
	}
	if plan.Kill {
		return ErrReplicaKilled
	}
	if next == nil {
		return nil
	}
	return next(ctx)
}
