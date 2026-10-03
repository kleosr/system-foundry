package auth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"systemsfoundry/platform"
)

type actorContextKey struct{}

func ActorFrom(ctx context.Context) (platform.Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(platform.Actor)
	return actor, ok
}

func WithActor(ctx context.Context, actor platform.Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func (p *Provider) Authenticate(audience string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" || strings.Contains(token, " ") {
			writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
			return
		}
		actor, err := p.signer.Verify(token, audience, p.now())
		if err != nil {
			writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

func (p *Provider) Require(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := ActorFrom(r.Context())
		if !ok {
			writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
			return
		}
		tenantID := r.PathValue("tenant")
		if actor.TenantID != tenantID {
			writeAuth(w, http.StatusNotFound, "NOT_FOUND", "Not found.")
			return
		}
		if !Can(actor, action, tenantID) {
			writeAuth(w, http.StatusForbidden, "FORBIDDEN", "Not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireOperator(opsTenant string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := ActorFrom(r.Context())
		if !ok {
			writeAuth(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required.")
			return
		}
		if !IsOperator(actor, opsTenant) {
			writeAuth(w, http.StatusForbidden, "FORBIDDEN", "Operator role required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Provider) ServeLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAuth(w, http.StatusMethodNotAllowed, "VALIDATION_FAILED", "POST required.")
		return
	}
	if r.Header.Get("Origin") != p.origin {
		writeAuth(w, http.StatusForbidden, "FORBIDDEN", "Origin rejected.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&body); err != nil {
		writeAuth(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Invalid login.")
		return
	}
	sessionID, err := p.Login(body.Email, body.Password, hostOnly(r.RemoteAddr))
	if err != nil {
		status := http.StatusUnauthorized
		code := "UNAUTHENTICATED"
		message := "Invalid email or password."
		if err == ErrRateLimited {
			status = http.StatusTooManyRequests
			code = "TOO_MANY_REQUESTS"
			message = "Too many attempts."
		}
		writeAuth(w, status, code, message)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sid",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(p.sessionTTL.Seconds()),
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]bool{"loggedIn": true}})
}

func hostOnly(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}
	return host
}

func writeAuth(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
