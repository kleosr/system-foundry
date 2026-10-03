package events

import (
	"context"
	"sync"
)

type Effect func(ctx context.Context, event OrderEvent) error

type Inbox struct {
	mu   sync.Mutex
	done map[string]struct{}
}

func NewInbox() *Inbox {
	return &Inbox{done: map[string]struct{}{}}
}

func (b *Inbox) ApplyOnce(id string, effect func() error) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.done[id]; ok {
		return false, nil
	}
	if err := effect(); err != nil {
		return false, err
	}
	b.done[id] = struct{}{}
	return true, nil
}

type Pipeline struct {
	log    *MemoryLog
	inbox  *Inbox
	effect Effect
}

func NewPipeline(log *MemoryLog, effect Effect) *Pipeline {
	return &Pipeline{log: log, inbox: NewInbox(), effect: effect}
}

func (p *Pipeline) Publish(event OrderEvent) error {
	return p.log.Append(event)
}

func (p *Pipeline) Deliver(ctx context.Context, event OrderEvent) error {
	applied, err := p.inbox.ApplyOnce(event.ID, func() error {
		return p.effect(ctx, event)
	})
	if err != nil {
		p.log.Nak(event.ID)
		return err
	}
	if !applied {
		p.log.Ack(event.ID)
		return nil
	}
	p.log.Ack(event.ID)
	return nil
}

func (p *Pipeline) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}
	applied := 0
	for i := 0; i < limit; i++ {
		event, ok := p.log.Pull()
		if !ok {
			return applied, nil
		}
		if err := p.Deliver(ctx, event); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}
