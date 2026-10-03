package streaming

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNoTenant = errors.New("message has no tenant")

type Message struct {
	TenantID string
	Event    string
	Data     string
}

type subscriber struct {
	id       string
	tenantID string
	ch       chan Message
	done     chan struct{}
}

type Subscription struct {
	Messages <-chan Message
	Closed   <-chan struct{}
	Cancel   func()
}

type Hub struct {
	mu          sync.Mutex
	buf         int
	slowAfter   time.Duration
	subscribers map[string]*subscriber
	evicted     atomic.Int64
}

func NewHub(buf int, slowAfter time.Duration) *Hub {
	if buf < 1 {
		buf = 1
	}
	if slowAfter <= 0 {
		slowAfter = 250 * time.Millisecond
	}
	return &Hub{buf: buf, slowAfter: slowAfter, subscribers: map[string]*subscriber{}}
}

func (h *Hub) Subscribe(tenantID string) Subscription {
	sub := &subscriber{id: newID(), tenantID: tenantID, ch: make(chan Message, h.buf), done: make(chan struct{})}
	h.mu.Lock()
	h.subscribers[sub.id] = sub
	h.mu.Unlock()
	return Subscription{Messages: sub.ch, Closed: sub.done, Cancel: func() { h.drop(sub.id) }}
}

func (h *Hub) Evicted() int64 {
	return h.evicted.Load()
}

func (h *Hub) Publish(ctx context.Context, msg Message) error {
	if msg.TenantID == "" {
		return ErrNoTenant
	}
	h.mu.Lock()
	targets := make([]*subscriber, 0, len(h.subscribers))
	for _, sub := range h.subscribers {
		if sub.tenantID == msg.TenantID {
			targets = append(targets, sub)
		}
	}
	h.mu.Unlock()
	for _, sub := range targets {
		if err := h.deliver(ctx, sub, msg); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) deliver(ctx context.Context, sub *subscriber, msg Message) error {
	select {
	case sub.ch <- msg:
		return nil
	case <-sub.done:
		return nil
	default:
	}
	timer := time.NewTimer(h.slowAfter)
	defer timer.Stop()
	select {
	case sub.ch <- msg:
		return nil
	case <-sub.done:
		return nil
	case <-timer.C:
		if h.drop(sub.id) {
			h.evicted.Add(1)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) drop(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub, ok := h.subscribers[id]
	if !ok {
		return false
	}
	delete(h.subscribers, id)
	close(sub.done)
	return true
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "subscriber"
	}
	return hex.EncodeToString(buf)
}
