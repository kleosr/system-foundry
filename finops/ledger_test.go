package finops

import (
	"testing"
	"time"
)

func TestCostMicrosUsesModelPrice(t *testing.T) {
	if got := CostMicros("primary", 2000); got != 60_000 {
		t.Fatalf("primary = %d", got)
	}
	if got := CostMicros("cheap", 2000); got != 2_000 {
		t.Fatalf("cheap = %d", got)
	}
	if got := CostMicros("cheap", 0); got != 0 {
		t.Fatalf("zero = %d", got)
	}
}

func TestRollupGroupsTenantEndpointAndModel(t *testing.T) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ledger := NewLedger(func() time.Time { return start.Add(time.Hour) }, 48*time.Hour)
	events := []Event{
		{TenantID: "acme", Endpoint: "/search", Model: "primary", USDMicros: 10, At: start},
		{TenantID: "acme", Endpoint: "/search", Model: "primary", USDMicros: 5, At: start.Add(time.Minute)},
		{TenantID: "acme", Endpoint: "/search", Model: "cheap", USDMicros: 1, At: start},
		{TenantID: "beta", Endpoint: "/search", Model: "primary", USDMicros: 7, At: start},
	}
	for _, event := range events {
		if err := ledger.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	rows := ledger.Rollup(start, start.Add(time.Hour))
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].TenantID != "acme" || rows[0].Model != "cheap" || rows[0].USDMicros != 1 {
		t.Fatalf("first = %+v", rows[0])
	}
	if rows[1].USDMicros != 15 || rows[1].Calls != 2 {
		t.Fatalf("primary = %+v", rows[1])
	}
}

func TestHourAnomalyFlagsTripleSpend(t *testing.T) {
	hour := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	ledger := NewLedger(func() time.Time { return hour }, 48*time.Hour)
	if err := ledger.Record(Event{TenantID: "acme", Endpoint: "/llm", Model: "primary", USDMicros: 10, At: hour.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(Event{TenantID: "acme", Endpoint: "/llm", Model: "primary", USDMicros: 40, At: hour.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	alert, current := ledger.HourAnomaly("acme", hour.Add(30*time.Minute))
	if !alert || current != 40 {
		t.Fatalf("alert=%v current=%d", alert, current)
	}
}

func TestRecordDropsEventsOutsideRetention(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	ledger := NewLedger(func() time.Time { return now }, time.Hour)
	err := ledger.Record(Event{TenantID: "acme", Endpoint: "/llm", Model: "primary", USDMicros: 10, At: now.Add(-2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	rows := ledger.Rollup(now.Add(-3*time.Hour), now)
	if len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRecordRejectsNegativeMoney(t *testing.T) {
	ledger := NewLedger(time.Now, time.Hour)
	err := ledger.Record(Event{TenantID: "acme", Endpoint: "/llm", Model: "primary", USDMicros: -1, At: time.Now()})
	if err == nil {
		t.Fatal("expected error")
	}
}
