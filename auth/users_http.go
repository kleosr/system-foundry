package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

var userTenantPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (p *Provider) EnsureUser(email, password, tenantID, role string) error {
	if _, exists := p.store.userByEmail(strings.ToLower(strings.TrimSpace(email))); exists {
		return nil
	}
	return p.CreateUser(email, password, tenantID, role)
}

func (p *Provider) ServeCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TenantID string `json:"tenantId"`
		Role     string `json:"role"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !userTenantPattern.MatchString(body.TenantID) || len(body.Password) < 12 {
		writeAuth(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Need email, a password of 12 or more characters, tenantId, and role.")
		return
	}
	err := p.CreateUser(body.Email, body.Password, body.TenantID, body.Role)
	if errors.Is(err, ErrInvalid) {
		writeAuth(w, http.StatusConflict, "CONFLICT", "User exists, or the email or role is invalid.")
		return
	}
	if err != nil {
		writeAuth(w, http.StatusServiceUnavailable, "UNAVAILABLE", "User store unavailable.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"ok":true,"data":{"created":true}}`))
}
