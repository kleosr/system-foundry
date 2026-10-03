package reconciliation

import (
	"context"
	"math/rand/v2"
	"time"

	"systemsfoundry/platform"
)

type Handler func(ctx context.Context, payload []byte) error

type Processor struct {
	store       *MemoryStore
	handler     Handler
	secret      []byte
	now         func() time.Time
	jitter      func() float64
	maxAttempts int
}

func NewProcessor(store *MemoryStore, handler Handler, secret []byte) *Processor {
	return &Processor{
		store:       store,
		handler:     handler,
		secret:      secret,
		now:         time.Now,
		jitter:      rand.Float64,
		maxAttempts: 8,
	}
}

func (p *Processor) Accept(ctx context.Context, provider, eventID string, payload []byte, signature string) platform.Result[Status] {
	if provider == "" || eventID == "" {
		return platform.FailFields[Status]("Invalid webhook.", map[string][]string{
			"eventId": []string{"required"},
		})
	}
	if !ValidSignature(payload, signature, p.secret) {
		return platform.Fail[Status](platform.CodeForbidden, "Signature rejected.")
	}
	if existing, ok := p.store.Get(ctx, provider, eventID); ok {
		return platform.Succeed(existing.Status)
	}
	p.store.Put(ctx, Record{
		Provider: provider,
		EventID:  eventID,
		Payload:  payload,
		Status:   StatusPending,
		NextAt:   p.now(),
	})
	return platform.Succeed(StatusPending)
}

func (p *Processor) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}
	due := p.store.ListDue(ctx, p.now(), limit)
	applied := 0
	for _, row := range due {
		if err := p.handler(ctx, row.Payload); err != nil {
			row.Attempts++
			row.LastError = "apply failed"
			if row.Attempts >= p.maxAttempts {
				row.Status = StatusDead
			} else {
				row.NextAt = p.now().Add(NextDelay(row.Attempts, p.jitter()))
			}
			p.store.Put(ctx, row)
			continue
		}
		row.Status = StatusApplied
		row.LastError = ""
		p.store.Put(ctx, row)
		applied++
	}
	return applied, nil
}

func (p *Processor) Replay(ctx context.Context, provider, eventID string) platform.Result[Status] {
	row, ok := p.store.Get(ctx, provider, eventID)
	if !ok {
		return platform.Fail[Status](platform.CodeNotFound, "Event not found.")
	}
	if row.Status != StatusDead {
		return platform.Fail[Status](platform.CodeConflict, "Event is not dead.")
	}
	row.Status = StatusPending
	row.Attempts = 0
	row.NextAt = p.now()
	row.LastError = ""
	p.store.Put(ctx, row)
	return platform.Succeed(StatusPending)
}
