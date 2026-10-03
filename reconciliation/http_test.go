package reconciliation

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func webhookMux(processor *Processor) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/{provider}", processor.ServeWebhook)
	mux.HandleFunc("GET /ops/webhooks/dead", processor.ServeDead)
	mux.HandleFunc("POST /ops/webhooks/{provider}/{id}/replay", processor.ServeReplay)
	return mux
}

func sendWebhook(mux http.Handler, eventID string, payload []byte, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("X-Webhook-Id", eventID)
	req.Header.Set("X-Webhook-Signature", signature)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestWebhookHTTPAcceptsDeadLettersAndReplays(t *testing.T) {
	secret := []byte("top-secret")
	failing := true
	processor := NewProcessor(NewMemoryStore(), func(context.Context, []byte) error {
		if failing {
			return errors.New("downstream down")
		}
		return nil
	}, secret)
	processor.jitter = func() float64 { return 0 }
	processor.maxAttempts = 1
	mux := webhookMux(processor)
	payload := []byte(`{"order":"o-1"}`)
	if rec := sendWebhook(mux, "evt-1", payload, "bad"); rec.Code != http.StatusForbidden {
		t.Fatalf("bad signature = %d", rec.Code)
	}
	if rec := sendWebhook(mux, "evt-1", payload, Sign(payload, secret)); rec.Code != http.StatusAccepted {
		t.Fatalf("accept = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := processor.Drain(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ops/webhooks/dead", nil))
	if !strings.Contains(rec.Body.String(), `"eventId":"evt-1"`) {
		t.Fatalf("dead = %s", rec.Body.String())
	}
	failing = false
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ops/webhooks/stripe/evt-1/replay", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("replay = %d %s", rec.Code, rec.Body.String())
	}
	applied, err := processor.Drain(context.Background(), 10)
	if err != nil || applied != 1 {
		t.Fatalf("applied=%d err=%v", applied, err)
	}
	if rec = sendWebhook(mux, "evt-1", payload, Sign(payload, secret)); !strings.Contains(rec.Body.String(), `"applied"`) {
		t.Fatalf("duplicate after apply = %s", rec.Body.String())
	}
}

func TestWebhookRejectsOversizedBody(t *testing.T) {
	processor := NewProcessor(NewMemoryStore(), func(context.Context, []byte) error { return nil }, []byte("s"))
	payload := bytes.Repeat([]byte("x"), maxWebhookBody+1)
	if rec := sendWebhook(webhookMux(processor), "evt-big", payload, Sign(payload, []byte("s"))); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
}
