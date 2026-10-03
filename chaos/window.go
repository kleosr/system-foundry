package chaos

import (
	"sync"
	"time"
)

const windowMinutes = 60

type minuteBucket struct {
	minute int64
	bad    int64
	total  int64
}

type Window struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets [windowMinutes]minuteBucket
}

func NewWindow(now func() time.Time) *Window {
	if now == nil {
		now = time.Now
	}
	return &Window{now: now}
}

func (w *Window) Observe(bad bool) {
	minute := w.now().Unix() / 60
	w.mu.Lock()
	defer w.mu.Unlock()
	slot := &w.buckets[minute%windowMinutes]
	if slot.minute != minute {
		*slot = minuteBucket{minute: minute}
	}
	slot.total++
	if bad {
		slot.bad++
	}
}

func (w *Window) Sample(span time.Duration) Sample {
	current := w.now().Unix() / 60
	oldest := current - int64(span/time.Minute) + 1
	w.mu.Lock()
	defer w.mu.Unlock()
	var sample Sample
	for _, slot := range w.buckets {
		if slot.minute >= oldest && slot.minute <= current {
			sample.Bad += slot.bad
			sample.Total += slot.total
		}
	}
	return sample
}
