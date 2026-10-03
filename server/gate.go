package server

import (
	"context"
	"errors"
)

var ErrSaturated = errors.New("server saturated")

type Gate struct {
	slots chan struct{}
	wait  chan struct{}
}

func NewGate(maxInFlight, maxWaiting int) (*Gate, error) {
	if maxInFlight < 1 {
		return nil, errors.New("max in flight must be positive")
	}
	if maxWaiting < 0 {
		return nil, errors.New("max waiting must be zero or positive")
	}
	return &Gate{
		slots: make(chan struct{}, maxInFlight),
		wait:  make(chan struct{}, maxWaiting),
	}, nil
}

func (g *Gate) Enter(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case g.slots <- struct{}{}:
		return nil
	default:
	}
	select {
	case g.wait <- struct{}{}:
	default:
		return ErrSaturated
	}
	defer func() { <-g.wait }()
	select {
	case g.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Gate) Leave() {
	<-g.slots
}
