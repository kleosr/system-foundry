package reconciliation

import (
	"context"
	"sync"
	"time"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusApplied Status = "applied"
	StatusDead    Status = "dead"
)

type Record struct {
	Provider  string
	EventID   string
	Payload   []byte
	Status    Status
	Attempts  int
	NextAt    time.Time
	LastError string
}

type MemoryStore struct {
	mu   sync.Mutex
	rows map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[string]Record{}}
}

func recordKey(provider, eventID string) string {
	return provider + "\x00" + eventID
}

func (s *MemoryStore) Get(_ context.Context, provider, eventID string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[recordKey(provider, eventID)]
	if ok {
		row.Payload = append([]byte(nil), row.Payload...)
	}
	return row, ok
}

func (s *MemoryStore) Put(_ context.Context, row Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row.Payload = append([]byte(nil), row.Payload...)
	s.rows[recordKey(row.Provider, row.EventID)] = row
}

func (s *MemoryStore) ListDue(_ context.Context, now time.Time, limit int) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, row := range s.rows {
		if row.Status != StatusPending || row.NextAt.After(now) {
			continue
		}
		row.Payload = append([]byte(nil), row.Payload...)
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *MemoryStore) ListDead(_ context.Context, limit int) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, row := range s.rows {
		if row.Status != StatusDead {
			continue
		}
		row.Payload = append([]byte(nil), row.Payload...)
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}
