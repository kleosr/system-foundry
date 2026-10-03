package ratelimit

import (
	"net/http"
	"strconv"

	"systemsfoundry/auth"
)

func (l *Limiter) Guard(next http.Handler) http.Handler {
	retryAfter := strconv.Itoa(max(1, int(l.window.Seconds())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth.ActorFrom(r.Context())
		if !ok {
			writeLimit(w, http.StatusUnauthorized, `{"ok":false,"error":{"code":"UNAUTHENTICATED","message":"Authentication required."}}`)
			return
		}
		decision, err := l.Allow(r.Context(), actor.TenantID)
		if err != nil {
			writeLimit(w, http.StatusForbidden, `{"ok":false,"error":{"code":"FORBIDDEN","message":"Tenant is not allowed."}}`)
			return
		}
		if decision.Degraded {
			w.Header().Set("X-RateLimit-Degraded", "1")
		}
		if !decision.Allowed {
			w.Header().Set("Retry-After", retryAfter)
			writeLimit(w, http.StatusTooManyRequests, `{"ok":false,"error":{"code":"TOO_MANY_REQUESTS","message":"Tenant rate limit reached."}}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeLimit(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
