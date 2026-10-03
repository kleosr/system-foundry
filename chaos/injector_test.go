package chaos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInjectedKillsBurnTheSLOAndPage(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	board := NewBoard(func() time.Time { return now })
	injector := NewInjector()
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := board.Observe(injector.Middleware(app))
	if !injector.Set(Rule{Kill: true, Ratio: 1, PathPrefix: "/v1/"}) {
		t.Fatal("rule rejected")
	}
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/search", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", rec.Code)
		}
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("unmatched path faulted: %d", rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	board.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slo", nil))
	var body struct {
		Data burnView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Data.Page || body.Data.ShortTotal != 200 || body.Data.ShortRate < 499 {
		t.Fatalf("burn = %+v", body.Data)
	}
}

func TestWindowForgetsMinutesOutsideTheSpan(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	window := NewWindow(func() time.Time { return now })
	window.Observe(true)
	now = now.Add(10 * time.Minute)
	window.Observe(false)
	if short := window.Sample(5 * time.Minute); short.Total != 1 || short.Bad != 0 {
		t.Fatalf("short = %+v", short)
	}
	if long := window.Sample(time.Hour); long.Total != 2 || long.Bad != 1 {
		t.Fatalf("long = %+v", long)
	}
	now = now.Add(2 * time.Hour)
	if gone := window.Sample(time.Hour); gone.Total != 0 {
		t.Fatalf("stale = %+v", gone)
	}
}

func TestInjectorRejectsUnboundedRules(t *testing.T) {
	injector := NewInjector()
	for _, body := range []string{
		`{"ratio":0,"pathPrefix":"/"}`,
		`{"ratio":1,"latencyMs":600000,"pathPrefix":"/"}`,
		`{"ratio":1,"pathPrefix":"v1"}`,
		`{"ratio":1,"pathPrefix":"/","extra":true}`,
	} {
		rec := httptest.NewRecorder()
		injector.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/chaos", strings.NewReader(body)))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s accepted: %d", body, rec.Code)
		}
	}
}
