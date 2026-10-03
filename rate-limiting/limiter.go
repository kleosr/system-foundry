package ratelimit

import (
	"context"
	"errors"
	"time"
)

var errBadLimit = errors.New("limit and window must be positive")
var errTenant = errors.New("tenant id is invalid")

type Store interface {
	Take(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

type Decision struct {
	Allowed  bool
	Degraded bool
}

type Limiter struct {
	store        Store
	local        *MemoryStore
	limit        int
	localLimit   int
	window       time.Duration
	storeTimeout time.Duration
}

func NewLimiter(store Store, limit, localLimit int, window, storeTimeout time.Duration) (*Limiter, error) {
	if store == nil || limit < 1 || localLimit < 1 || window <= 0 {
		return nil, errBadLimit
	}
	if storeTimeout <= 0 {
		storeTimeout = 50 * time.Millisecond
	}
	return &Limiter{
		store:        store,
		local:        NewMemoryStore(time.Now),
		limit:        limit,
		localLimit:   localLimit,
		window:       window,
		storeTimeout: storeTimeout,
	}, nil
}

func (l *Limiter) Allow(ctx context.Context, tenantID string) (Decision, error) {
	key, ok := Key(tenantID)
	if !ok {
		return Decision{}, errTenant
	}
	storeCtx, cancel := context.WithTimeout(ctx, l.storeTimeout)
	defer cancel()
	allowed, err := l.store.Take(storeCtx, key, l.limit, l.window)
	if err == nil {
		return Decision{Allowed: allowed}, nil
	}
	localAllowed, localErr := l.local.Take(context.Background(), key, l.localLimit, l.window)
	if localErr != nil {
		return Decision{}, localErr
	}
	return Decision{Allowed: localAllowed, Degraded: true}, nil
}
