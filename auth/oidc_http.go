package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"systemsfoundry/platform"
)

const sessionCookie = "sid"

func (p *Provider) SessionActor(sessionID string) (platform.Actor, bool) {
	session, ok := p.store.session(sessionID)
	if !ok || !session.Expires.After(p.now()) {
		return platform.Actor{}, false
	}
	user, ok := p.store.userByID(session.UserID)
	if !ok {
		return platform.Actor{}, false
	}
	return platform.Actor{Subject: user.ID, TenantID: user.TenantID, Role: user.Role}, true
}

func (p *Provider) AuthenticateSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
			return
		}
		actor, ok := p.SessionActor(cookie.Value)
		if !ok {
			writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

func (p *Provider) ServeLogout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != p.origin {
		writeAuth(w, http.StatusForbidden, "FORBIDDEN", "Origin rejected.")
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if err = p.Logout(cookie.Value); err != nil {
			writeAuth(w, http.StatusServiceUnavailable, "UNAVAILABLE", "Logout failed.")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"data":{"loggedIn":false}}`))
}

func (p *Provider) ServeDiscovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                p.origin,
		"authorization_endpoint":                p.origin + "/authorize",
		"token_endpoint":                        p.origin + "/token",
		"jwks_uri":                              p.origin + "/jwks.json",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"subject_types_supported":               []string{"public"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

func (p *Provider) ServeJWKS(w http.ResponseWriter, _ *http.Request) {
	keys, err := p.signer.JWKS()
	if err != nil {
		writeAuth(w, http.StatusInternalServerError, "UNAVAILABLE", "Keys unavailable.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(keys)
}

func (p *Provider) ServeAuthorize(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
		return
	}
	query := r.URL.Query()
	if query.Get("response_type") != "code" {
		writeAuth(w, http.StatusBadRequest, "VALIDATION_FAILED", "response_type must be code.")
		return
	}
	redirect := query.Get("redirect_uri")
	code, err := p.Authorize(cookie.Value, query.Get("client_id"), redirect, query.Get("state"),
		query.Get("code_challenge"), query.Get("code_challenge_method"), query.Get("nonce"))
	if errors.Is(err, ErrUnauthenticated) {
		writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
		return
	}
	if err != nil {
		writeAuth(w, http.StatusBadRequest, "VALIDATION_FAILED", "Invalid authorization request.")
		return
	}
	target, err := url.Parse(redirect)
	if err != nil {
		writeAuth(w, http.StatusBadRequest, "VALIDATION_FAILED", "Invalid authorization request.")
		return
	}
	params := target.Query()
	params.Set("code", code)
	params.Set("state", query.Get("state"))
	target.RawQuery = params.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (p *Provider) ServeToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	clientID := r.PostForm.Get("client_id")
	var set TokenSet
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		set, err = p.ExchangeCode(clientID, r.PostForm.Get("code"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
	case "refresh_token":
		set, err = p.Refresh(clientID, r.PostForm.Get("refresh_token"))
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  set.AccessToken,
		"token_type":    set.TokenType,
		"expires_in":    set.ExpiresIn,
		"refresh_token": set.RefreshToken,
		"id_token":      set.IDToken,
	})
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
