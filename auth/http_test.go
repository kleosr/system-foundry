package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMissingTokenIs401(t *testing.T) {
	provider, _ := testProvider(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tenants/acme/documents", nil)
	provider.Authenticate("foundry-api", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTenantAndRoleMatrix(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	if err := provider.CreateUser("agent@example.com", "correct-horse", "acme", "agent"); err != nil {
		t.Fatal(err)
	}
	if err := provider.CreateUser("beta@example.com", "correct-horse", "beta", "owner"); err != nil {
		t.Fatal(err)
	}
	owner := tokensFor(t, provider)
	agentSession, err := provider.Login("agent@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	agentCode, err := provider.Authorize(agentSession, "cli", "https://app.example/callback", "state-1", challengeFor(verifier), "S256", "nonce-a")
	if err != nil {
		t.Fatal(err)
	}
	agentSet, err := provider.ExchangeCode("cli", agentCode, "https://app.example/callback", verifier)
	if err != nil {
		t.Fatal(err)
	}
	betaSession, err := provider.Login("beta@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	betaCode, err := provider.Authorize(betaSession, "cli", "https://app.example/callback", "state-1", challengeFor(verifier), "S256", "nonce-b")
	if err != nil {
		t.Fatal(err)
	}
	betaSet, err := provider.ExchangeCode("cli", betaCode, "https://app.example/callback", verifier)
	if err != nil {
		t.Fatal(err)
	}
	var wrote bool
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrote = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux := http.NewServeMux()
	mux.Handle("GET /tenants/{tenant}/documents", provider.Authenticate("foundry-api", provider.Require(ActionRead, okHandler)))
	mux.Handle("POST /tenants/{tenant}/documents", provider.Authenticate("foundry-api", provider.Require(ActionWrite, okHandler)))

	wrote = false
	status := callAuth(mux, http.MethodGet, "/tenants/beta/documents", betaSet.AccessToken)
	if status != http.StatusNoContent || !wrote {
		t.Fatalf("owner read status=%d wrote=%v", status, wrote)
	}
	wrote = false
	status = callAuth(mux, http.MethodGet, "/tenants/acme/documents", betaSet.AccessToken)
	if status != http.StatusNotFound || wrote {
		t.Fatalf("cross read status=%d wrote=%v", status, wrote)
	}
	wrote = false
	status = callAuth(mux, http.MethodPost, "/tenants/acme/documents", betaSet.AccessToken)
	if status != http.StatusNotFound || wrote {
		t.Fatalf("cross write status=%d wrote=%v", status, wrote)
	}
	wrote = false
	status = callAuth(mux, http.MethodPost, "/tenants/acme/documents", agentSet.AccessToken)
	if status != http.StatusForbidden || wrote {
		t.Fatalf("agent write status=%d wrote=%v", status, wrote)
	}
	wrote = false
	status = callAuth(mux, http.MethodGet, "/tenants/acme/documents", agentSet.AccessToken)
	if status != http.StatusNoContent || !wrote {
		t.Fatalf("agent read status=%d wrote=%v", status, wrote)
	}
	wrote = false
	status = callAuth(mux, http.MethodPost, "/tenants/acme/documents", owner.AccessToken)
	if status != http.StatusNoContent || !wrote {
		t.Fatalf("owner write status=%d wrote=%v", status, wrote)
	}
}

func TestLoginCookieAndOrigin(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	body := `{"email":"ada@example.com","password":"correct-horse"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	provider.ServeLogin(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Origin", "https://foundry.local")
	rec = httptest.NewRecorder()
	provider.ServeLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "Secure") || !strings.Contains(cookie, "SameSite=Lax") {
		t.Fatalf("cookie = %s", cookie)
	}
}

func callAuth(handler http.Handler, method, path, token string) int {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	_, _ = io.Copy(io.Discard, rec.Body)
	return rec.Code
}
