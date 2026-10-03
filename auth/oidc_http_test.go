package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func oidcMux(provider *Provider) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", provider.ServeLogin)
	mux.HandleFunc("GET /authorize", provider.ServeAuthorize)
	mux.HandleFunc("POST /token", provider.ServeToken)
	mux.Handle("GET /me", provider.AuthenticateSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, _ := ActorFrom(r.Context())
		_, _ = w.Write([]byte(actor.TenantID))
	})))
	return mux
}

func loginCookie(t *testing.T, mux http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"ada@example.com","password":"correct-horse"}`))
	req.Header.Set("Origin", "https://foundry.local")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie {
			return cookie
		}
	}
	t.Fatalf("no session cookie, status %d", rec.Code)
	return nil
}

func authorizeURL(redirect string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {"cli"},
		"redirect_uri":          {redirect},
		"state":                 {"state-1"},
		"nonce":                 {"nonce-1"},
		"code_challenge":        {challengeFor(verifier)},
		"code_challenge_method": {"S256"},
	}
	return "/authorize?" + params.Encode()
}

func postToken(mux http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCodeFlowOverHTTP(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	mux := oidcMux(provider)
	cookie := loginCookie(t, mux)

	req := httptest.NewRequest(http.MethodGet, authorizeURL("https://app.example/callback"), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d %s", rec.Code, rec.Body.String())
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || location.Query().Get("state") != "state-1" {
		t.Fatalf("location = %s", rec.Header().Get("Location"))
	}
	code := location.Query().Get("code")
	exchange := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"cli"}, "code": {code},
		"redirect_uri": {"https://app.example/callback"}, "code_verifier": {verifier},
	}
	rec = postToken(mux, exchange)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token = %d %s", rec.Code, rec.Body.String())
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil || tokens.AccessToken == "" {
		t.Fatalf("tokens = %s", rec.Body.String())
	}
	if rec = postToken(mux, exchange); rec.Code != http.StatusBadRequest {
		t.Fatalf("code reuse = %d", rec.Code)
	}
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {"cli"}, "refresh_token": {tokens.RefreshToken}}
	if rec = postToken(mux, refresh); rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d", rec.Code)
	}
	if rec = postToken(mux, refresh); rec.Code != http.StatusBadRequest {
		t.Fatalf("refresh reuse = %d", rec.Code)
	}
}

func TestAuthorizeRejectsUnregisteredRedirectWithoutRedirecting(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	mux := oidcMux(provider)
	cookie := loginCookie(t, mux)
	req := httptest.NewRequest(http.MethodGet, authorizeURL("https://evil.example/callback"), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("status=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAuthorizeWithoutSessionIs401(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	rec := httptest.NewRecorder()
	oidcMux(provider).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, authorizeURL("https://app.example/callback"), nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTokenRejectsWrongVerifier(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	sessionID, err := provider.Login("ada@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, err := provider.Authorize(sessionID, "cli", "https://app.example/callback", "s", challengeFor(verifier), "S256", "n")
	if err != nil {
		t.Fatal(err)
	}
	rec := postToken(oidcMux(provider), url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"cli"}, "code": {code},
		"redirect_uri": {"https://app.example/callback"}, "code_verifier": {strings.Repeat("z", 64)},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionAuthRejectsMissingAndLoggedOutCookie(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	mux := oidcMux(provider)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing cookie = %d", rec.Code)
	}
	cookie := loginCookie(t, mux)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "acme" {
		t.Fatalf("session = %d %s", rec.Code, rec.Body.String())
	}
	provider.Logout(cookie.Value)
	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after logout = %d", rec.Code)
	}
}
