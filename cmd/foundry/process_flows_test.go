package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"
)

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestProcessSearchAndAccessBoundaries(t *testing.T) {
	proc := startLocal(t, nil)
	owner, agent, beta := proc.token("owner@acme.test"), proc.token("agent@acme.test"), proc.token("owner@beta.test")
	if status, body := proc.do(http.MethodPost, "/v1/documents", owner, `{"id":"d1","title":"refund policy","body":"refunds within 30 days"}`, nil); status != http.StatusOK {
		t.Fatalf("index = %d %s", status, body)
	}
	if status, _ := proc.do(http.MethodPost, "/v1/documents", agent, `{"id":"d2","title":"x","body":"y"}`, nil); status != http.StatusForbidden {
		t.Fatalf("agent index = %d", status)
	}
	if status, body := proc.do(http.MethodGet, "/v1/search?q=refund", agent, "", nil); status != http.StatusOK || !strings.Contains(body, `"d1"`) {
		t.Fatalf("agent search = %d %s", status, body)
	}
	if _, body := proc.do(http.MethodGet, "/v1/search?q=refund", beta, "", nil); strings.Contains(body, `"d1"`) {
		t.Fatalf("beta saw acme document: %s", body)
	}
	if status, _ := proc.do(http.MethodGet, "/v1/search?q=refund", "", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous search = %d", status)
	}
	if status, _ := proc.do(http.MethodGet, "/v1/costs", "", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous costs = %d", status)
	}
	if status, _ := proc.do(http.MethodPut, "/ops/chaos", owner, `{"ratio":1,"kill":true,"pathPrefix":"/"}`, nil); status != http.StatusNotFound {
		t.Fatalf("chaos without flag = %d", status)
	}
}

func TestProcessStreamsTokensAndOrderChangesPerTenant(t *testing.T) {
	proc := startLocal(t, nil)
	owner := proc.token("owner@acme.test")
	acmeStream := proc.openStream("owner@acme.test")
	betaStream := proc.openStream("owner@beta.test")
	status, body := proc.do(http.MethodPost, "/v1/complete", owner, `{"prompt":"hello streaming world"}`, nil)
	if status != http.StatusOK || !strings.Contains(body, "primary: hello streaming world") {
		t.Fatalf("complete = %d %s", status, body)
	}
	readUntil(t, acmeStream, "event: token")
	readUntil(t, acmeStream, `"type":"completion"`)
	headers := map[string]string{"Idempotency-Key": "order-key-0001"}
	for i := 0; i < 2; i++ {
		if status, body = proc.do(http.MethodPost, "/v1/orders/o-77/events", owner, `{"kind":"order.placed"}`, headers); status != http.StatusAccepted {
			t.Fatalf("append %d = %d %s", i, status, body)
		}
	}
	headers["Idempotency-Key"] = "order-key-0002"
	if status, body = proc.do(http.MethodPost, "/v1/orders/o-77/events", owner, `{"kind":"order.paid"}`, headers); status != http.StatusAccepted {
		t.Fatalf("paid = %d %s", status, body)
	}
	readUntil(t, acmeStream, `"status":"paid"`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, body = proc.do(http.MethodGet, "/v1/orders/o-77", owner, "", nil)
		if strings.Contains(body, `"status":"paid"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("order view = %d %s", status, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if strings.Count(body, "acme-order-key") != 2 {
		t.Fatalf("duplicate event applied: %s", body)
	}
	if status, body = proc.do(http.MethodGet, "/v1/orders/o-77", proc.token("owner@beta.test"), "", nil); status != http.StatusNotFound {
		t.Fatalf("beta read acme order = %d %s", status, body)
	}
	if status, body = proc.do(http.MethodGet, "/v1/costs", owner, "", nil); status != http.StatusOK || !strings.Contains(body, `"Model":"primary"`) {
		t.Fatalf("costs = %d %s", status, body)
	}
	assertNoTenantLeak(t, betaStream)
}

func assertNoTenantLeak(t *testing.T, stream *bufio.Reader) {
	t.Helper()
	leaked := make(chan string, 1)
	go func() {
		for {
			line, err := stream.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "event:") {
				leaked <- line
				return
			}
		}
	}()
	select {
	case line := <-leaked:
		t.Fatalf("beta stream received %q", line)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestProcessChaosAndWebhooksForOperators(t *testing.T) {
	proc := startLocal(t, map[string]string{"FOUNDRY_CHAOS_ENABLED": "true", "FOUNDRY_WEBHOOK_SECRET": "whsec-test"})
	owner, operator := proc.token("owner@acme.test"), proc.token("operator@ops.test")
	if status, _ := proc.do(http.MethodPut, "/ops/chaos", owner, `{"ratio":1,"kill":true,"pathPrefix":"/v1/search"}`, nil); status != http.StatusForbidden {
		t.Fatalf("tenant owner set chaos = %d", status)
	}
	if status, body := proc.do(http.MethodPut, "/ops/chaos", operator, `{"ratio":1,"kill":true,"pathPrefix":"/v1/search"}`, nil); status != http.StatusOK {
		t.Fatalf("operator set chaos = %d %s", status, body)
	}
	for i := 0; i < 20; i++ {
		if status, _ := proc.do(http.MethodGet, "/v1/search?q=x", owner, "", nil); status != http.StatusServiceUnavailable {
			t.Fatalf("injected kill = %d", status)
		}
	}
	if _, body := proc.do(http.MethodGet, "/slo", "", "", nil); !strings.Contains(body, `"page":true`) {
		t.Fatalf("slo did not page: %s", body)
	}
	if status, _ := proc.do(http.MethodDelete, "/ops/chaos", operator, "", nil); status != http.StatusOK {
		t.Fatalf("clear chaos = %d", status)
	}
	if status, _ := proc.do(http.MethodGet, "/v1/search?q=x", owner, "", nil); status != http.StatusOK {
		t.Fatalf("after clear = %d", status)
	}
	if status, _ := proc.do(http.MethodPost, "/webhooks/stripe", "", `{}`, map[string]string{"X-Webhook-Id": "evt-1", "X-Webhook-Signature": "bad"}); status != http.StatusForbidden {
		t.Fatalf("unsigned webhook = %d", status)
	}
	if status, _ := proc.do(http.MethodGet, "/ops/webhooks/dead", owner, "", nil); status != http.StatusForbidden {
		t.Fatalf("tenant owner read dead letters = %d", status)
	}
}
