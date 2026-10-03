package finops

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrInvalidCost = errors.New("cost event is invalid")

type Event struct {
	TenantID  string
	Endpoint  string
	Model     string
	USDMicros int64
	At        time.Time
}

type Rollup struct {
	TenantID  string
	Endpoint  string
	Model     string
	USDMicros int64
	Calls     int
}

type costKey struct {
	TenantID string
	Endpoint string
	Model    string
}

type hourKey struct {
	hour time.Time
	cost costKey
}

type Ledger struct {
	mu       sync.Mutex
	now      func() time.Time
	retain   time.Duration
	buckets  map[hourKey]*Rollup
	prunedAt time.Time
}

func NewLedger(now func() time.Time, retain time.Duration) *Ledger {
	if now == nil {
		now = time.Now
	}
	if retain <= 0 {
		retain = 35 * 24 * time.Hour
	}
	return &Ledger{now: now, retain: retain, buckets: map[hourKey]*Rollup{}}
}

func (l *Ledger) Record(event Event) error {
	if event.TenantID == "" || event.Endpoint == "" || event.USDMicros < 0 || event.At.IsZero() {
		return ErrInvalidCost
	}
	now := l.now()
	cutoff := now.Add(-l.retain)
	if event.At.Before(cutoff) {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.prunedAt) >= time.Hour {
		l.prune(cutoff.Truncate(time.Hour))
		l.prunedAt = now
	}
	cost := costKey{TenantID: event.TenantID, Endpoint: event.Endpoint, Model: event.Model}
	key := hourKey{hour: event.At.UTC().Truncate(time.Hour), cost: cost}
	row := l.buckets[key]
	if row == nil {
		row = &Rollup{TenantID: cost.TenantID, Endpoint: cost.Endpoint, Model: cost.Model}
		l.buckets[key] = row
	}
	row.USDMicros += event.USDMicros
	row.Calls++
	return nil
}

func (l *Ledger) prune(oldest time.Time) {
	for key := range l.buckets {
		if key.hour.Before(oldest) {
			delete(l.buckets, key)
		}
	}
}

func (l *Ledger) Rollup(from, to time.Time) []Rollup {
	from = from.UTC().Truncate(time.Hour)
	l.mu.Lock()
	grouped := map[costKey]Rollup{}
	for key, bucket := range l.buckets {
		if key.hour.Before(from) || !key.hour.Before(to) {
			continue
		}
		row := grouped[key.cost]
		row.TenantID, row.Endpoint, row.Model = bucket.TenantID, bucket.Endpoint, bucket.Model
		row.USDMicros += bucket.USDMicros
		row.Calls += bucket.Calls
		grouped[key.cost] = row
	}
	l.mu.Unlock()
	rows := make([]Rollup, 0, len(grouped))
	for _, row := range grouped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TenantID != rows[j].TenantID {
			return rows[i].TenantID < rows[j].TenantID
		}
		if rows[i].Endpoint != rows[j].Endpoint {
			return rows[i].Endpoint < rows[j].Endpoint
		}
		return rows[i].Model < rows[j].Model
	})
	return rows
}

func (l *Ledger) HourAnomaly(tenantID string, now time.Time) (bool, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var current int64
	var previous int64
	hourStart := now.UTC().Truncate(time.Hour)
	dayStart := hourStart.Add(-24 * time.Hour)
	seen := map[time.Time]struct{}{}
	for key, bucket := range l.buckets {
		if key.cost.TenantID != tenantID || key.hour.Before(dayStart) || key.hour.After(hourStart) {
			continue
		}
		if key.hour.Equal(hourStart) {
			current += bucket.USDMicros
			continue
		}
		previous += bucket.USDMicros
		seen[key.hour] = struct{}{}
	}
	previousHours := len(seen)
	if previousHours == 0 {
		return false, current
	}
	mean := previous / int64(previousHours)
	if mean == 0 {
		return current > 0, current
	}
	return current > mean*3, current
}
