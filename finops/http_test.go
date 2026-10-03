package finops

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

func TestRegressionCostsReadAnyTenantFromQueryString(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ledger := NewLedger(func() time.Time { return now }, time.Hour*48)
	if err := ledger.Record(Event{TenantID: "acme", Endpoint: "/v1/complete", Model: "primary", USDMicros: 900, At: now}); err != nil {
		t.Fatal(err)
	}
	beta := platform.Actor{Subject: "u-beta", TenantID: "beta", Role: platform.RoleOwner}
	req := httptest.NewRequest(http.MethodGet, "/costs?tenant=acme", nil)
	req = req.WithContext(auth.WithActor(req.Context(), beta))
	rec := httptest.NewRecorder()
	ledger.ServeCosts(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "acme") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	acme := platform.Actor{Subject: "u-acme", TenantID: "acme", Role: platform.RoleAgent}
	req = httptest.NewRequest(http.MethodGet, "/costs", nil)
	req = req.WithContext(auth.WithActor(req.Context(), acme))
	rec = httptest.NewRecorder()
	ledger.ServeCosts(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"USDMicros":900`) {
		t.Fatalf("own tenant status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCostsWithoutActorIsForbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	NewLedger(time.Now, time.Hour).ServeCosts(rec, httptest.NewRequest(http.MethodGet, "/costs?tenant=acme", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}
