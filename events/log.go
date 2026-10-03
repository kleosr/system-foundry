package events

import "sync"

type delivery struct {
	event    OrderEvent
	attempts int
	acked    bool
	dead     bool
}

type MemoryLog struct {
	mu         sync.Mutex
	maxDeliver int
	rows       []delivery
	dead       []OrderEvent
}

func NewMemoryLog(maxDeliver int) *MemoryLog {
	if maxDeliver < 1 {
		maxDeliver = 5
	}
	return &MemoryLog{maxDeliver: maxDeliver}
}

func (l *MemoryLog) Append(event OrderEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, row := range l.rows {
		if row.event.ID == event.ID {
			return ErrDuplicate
		}
	}
	for _, row := range l.dead {
		if row.ID == event.ID {
			return ErrDuplicate
		}
	}
	l.rows = append(l.rows, delivery{event: cloneEvent(event)})
	return nil
}

func (l *MemoryLog) Pull() (OrderEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, row := range l.rows {
		if !row.acked && !row.dead {
			return cloneEvent(row.event), true
		}
	}
	return OrderEvent{}, false
}

func (l *MemoryLog) Ack(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.rows {
		if l.rows[i].event.ID == id && !l.rows[i].dead {
			l.rows[i].acked = true
			return true
		}
	}
	return false
}

func (l *MemoryLog) Nak(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.rows {
		if l.rows[i].event.ID != id || l.rows[i].acked || l.rows[i].dead {
			continue
		}
		l.rows[i].attempts++
		if l.rows[i].attempts >= l.maxDeliver {
			l.rows[i].dead = true
			l.dead = append(l.dead, cloneEvent(l.rows[i].event))
		}
		return true
	}
	return false
}

func (l *MemoryLog) Dead() []OrderEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]OrderEvent, len(l.dead))
	copy(out, l.dead)
	return out
}

func cloneEvent(event OrderEvent) OrderEvent {
	event.Payload = append([]byte(nil), event.Payload...)
	return event
}
