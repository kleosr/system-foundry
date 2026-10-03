package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

func guardedCall(handler http.Handler, tenantID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/search", nil)
	req = req.WithContext(auth.WithActor(req.Context(), platform.Actor{Subject: "u", TenantID: tenantID, Role: platform.RoleOwner}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestGuardCapsOneTenantWithoutTouchingAnother(t *testing.T) {
	limiter, err := NewLimiter(NewMemoryStore(time.Now), 2, 1, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	handler := limiter.Guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i := 0; i < 2; i++ {
		if rec := guardedCall(handler, "acme"); rec.Code != http.StatusNoContent {
			t.Fatalf("call %d = %d", i, rec.Code)
		}
	}
	rec := guardedCall(handler, "acme")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("over limit = %d retry=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec = guardedCall(handler, "beta"); rec.Code != http.StatusNoContent {
		t.Fatalf("other tenant = %d", rec.Code)
	}
}

func TestGuardDegradesToLocalCapWhenRedisPartitions(t *testing.T) {
	server := miniredis.RunT(t)
	limiter, err := NewLimiter(NewRedisStore(server.Addr(), 20*time.Millisecond), 100, 1, time.Minute, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	handler := limiter.Guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if rec := guardedCall(handler, "acme"); rec.Code != http.StatusNoContent || rec.Header().Get("X-RateLimit-Degraded") != "" {
		t.Fatalf("healthy = %d degraded=%q", rec.Code, rec.Header().Get("X-RateLimit-Degraded"))
	}
	server.Close()
	rec := guardedCall(handler, "acme")
	if rec.Code != http.StatusNoContent || rec.Header().Get("X-RateLimit-Degraded") != "1" {
		t.Fatalf("partitioned first = %d degraded=%q", rec.Code, rec.Header().Get("X-RateLimit-Degraded"))
	}
	if rec = guardedCall(handler, "acme"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("partitioned second = %d", rec.Code)
	}
}
