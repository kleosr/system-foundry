package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"systemsfoundry/events"
)

func writeSharedIdentity(t *testing.T) (keyFile, passwordFile, password string) {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyFile = filepath.Join(dir, "signing.pem")
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	password = hex.EncodeToString(raw)
	passwordFile = filepath.Join(dir, "operator")
	if err = os.WriteFile(passwordFile, []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return keyFile, passwordFile, password
}

func startOrdersWorker(t *testing.T, url string) {
	t.Helper()
	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	topology, err := events.EnsureTopology(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	worker := events.NewWorker(js, topology.Views, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() {
		_ = worker.Run(ctx, topology.Consumer)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		conn.Close()
	})
}

func TestSharedModeOnRealPostgresAndNATS(t *testing.T) {
	dsn, natsURL := os.Getenv("FOUNDRY_TEST_POSTGRES_DSN"), os.Getenv("FOUNDRY_TEST_NATS_URL")
	if dsn == "" || natsURL == "" {
		t.Skip("FOUNDRY_TEST_POSTGRES_DSN and FOUNDRY_TEST_NATS_URL are required")
	}
	keyFile, passwordFile, password := writeSharedIdentity(t)
	suffix := hex.EncodeToString([]byte(time.Now().Format("150405.000")))[:12]
	operatorEmail, tenantID := "op-"+suffix+"@example.com", "t"+suffix
	env := func() map[string]string {
		return map[string]string{
			"FOUNDRY_MODE": "shared", "FOUNDRY_POSTGRES_DSN": dsn, "FOUNDRY_NATS_URL": natsURL,
			"FOUNDRY_REDIS_ADDR": "127.0.0.1:1", "FOUNDRY_SIGNING_KEY_FILE": keyFile,
			"FOUNDRY_BOOTSTRAP_OPERATOR_EMAIL": operatorEmail, "FOUNDRY_BOOTSTRAP_OPERATOR_PASSWORD_FILE": passwordFile,
		}
	}
	startOrdersWorker(t, natsURL)
	first := startProcess(t, "127.0.0.1:0", env())
	first.password = password
	operator := first.token(operatorEmail)
	userBody := `{"email":"owner@` + tenantID + `.test","password":"` + password + `","tenantId":"` + tenantID + `","role":"owner"}`
	if status, body := first.do(http.MethodPost, "/ops/users", operator, userBody, nil); status != http.StatusCreated {
		t.Fatalf("create user = %d %s", status, body)
	}
	owner := first.token("owner@" + tenantID + ".test")
	status, body := first.do(http.MethodPost, "/v1/documents", owner, `{"id":"d1","title":"postgres rls","body":"row level security"}`, nil)
	if status != http.StatusOK {
		t.Fatalf("index = %d %s", status, body)
	}
	resp, err := first.client.Do(mustRequest(t, http.MethodGet, first.server.URL+"/v1/search?q=security", owner))
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("X-RateLimit-Degraded") != "1" {
		t.Fatalf("search with Redis down: err=%v status=%v degraded=%q", err, resp.StatusCode, resp.Header.Get("X-RateLimit-Degraded"))
	}
	resp.Body.Close()
	headers := map[string]string{"Idempotency-Key": "shared-" + suffix}
	if status, body = first.do(http.MethodPost, "/v1/orders/o-1/events", owner, `{"kind":"order.paid"}`, headers); status != http.StatusAccepted {
		t.Fatalf("order event = %d %s", status, body)
	}
	waitForBody(t, first, "/v1/orders/o-1", owner, `"status":"paid"`)
	address := first.server.Listener.Addr().String()
	first.stop()

	second := startProcess(t, address, env())
	if status, body = second.do(http.MethodGet, "/v1/search?q=security", owner, "", nil); status != http.StatusOK || !strings.Contains(body, `"d1"`) {
		t.Fatalf("token or data lost across restart: %d %s", status, body)
	}
	if status, body = second.do(http.MethodGet, "/readyz", "", "", nil); status != http.StatusOK {
		t.Fatalf("readyz = %d %s", status, body)
	}
}

func mustRequest(t *testing.T, method, url, token string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func waitForBody(t *testing.T, proc *liveProcess, path, token, needle string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, body := proc.do(http.MethodGet, path, token, "", nil)
		if strings.Contains(body, needle) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %d %s", path, status, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
