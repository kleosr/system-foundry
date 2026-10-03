package finops

import (
	"testing"
	"time"
)

func TestAlerterFiresOncePerTenantHourOnSpike(t *testing.T) {
	hour := time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	ledger := NewLedger(func() time.Time { return hour }, 48*time.Hour)
	var fired []Anomaly
	alerter := NewAlerter(ledger, func(a Anomaly) { fired = append(fired, a) })
	for h := 1; h <= 3; h++ {
		if err := alerter.Record(Event{TenantID: "acme", Endpoint: "/v1/complete", Model: "m", USDMicros: 100, At: hour.Add(-time.Duration(h) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(fired) != 0 {
		t.Fatalf("baseline fired: %+v", fired)
	}
	for i := 0; i < 5; i++ {
		if err := alerter.Record(Event{TenantID: "acme", Endpoint: "/v1/complete", Model: "m", USDMicros: 100, At: hour.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(fired) != 1 || fired[0].HourMicros != 400 || fired[0].TenantID != "acme" {
		t.Fatalf("fired = %+v", fired)
	}
}

func TestPriceTableChargesUnknownModelAtHighestPrice(t *testing.T) {
	table := PriceTable{"gpt-big": 5000, "gpt-small": 150}
	if got := table.CostMicros("gpt-small", 2000); got != 300 {
		t.Fatalf("small = %d", got)
	}
	if got := table.CostMicros("surprise", 1000); got != 5000 {
		t.Fatalf("unknown = %d", got)
	}
}
