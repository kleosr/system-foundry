package finops

import (
	"sync"
	"time"
)

type Anomaly struct {
	TenantID    string    `json:"tenantId"`
	Hour        time.Time `json:"hour"`
	HourMicros  int64     `json:"hourMicros"`
	TriggeredBy string    `json:"triggeredBy"`
}

type Alerter struct {
	ledger *Ledger
	notify func(Anomaly)
	mu     sync.Mutex
	hour   time.Time
	fired  map[string]struct{}
}

func NewAlerter(ledger *Ledger, notify func(Anomaly)) *Alerter {
	return &Alerter{ledger: ledger, notify: notify, fired: map[string]struct{}{}}
}

func (a *Alerter) Record(event Event) error {
	if err := a.ledger.Record(event); err != nil {
		return err
	}
	alert, current := a.ledger.HourAnomaly(event.TenantID, event.At)
	if !alert {
		return nil
	}
	hour := event.At.UTC().Truncate(time.Hour)
	a.mu.Lock()
	if !hour.Equal(a.hour) {
		a.hour = hour
		a.fired = map[string]struct{}{}
	}
	_, already := a.fired[event.TenantID]
	a.fired[event.TenantID] = struct{}{}
	a.mu.Unlock()
	if !already {
		a.notify(Anomaly{TenantID: event.TenantID, Hour: hour, HourMicros: current, TriggeredBy: event.Endpoint})
	}
	return nil
}
