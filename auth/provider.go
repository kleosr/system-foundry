package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"systemsfoundry/platform"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrInvalid         = errors.New("invalid")
	ErrRateLimited     = errors.New("rate limited")
)

type TokenSet struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
	TokenType    string
}

type Provider struct {
	store      credentialStore
	signer     *Signer
	throttle   *Throttle
	params     Params
	dummy      string
	now        func() time.Time
	origin     string
	accessTTL  time.Duration
	refreshTTL time.Duration
	codeTTL    time.Duration
	sessionTTL time.Duration
}

func NewProvider(origin string, now func() time.Time, params Params) (*Provider, error) {
	signer, err := NewSigner(origin)
	if err != nil {
		return nil, err
	}
	return newProvider(origin, now, params, signer, NewMemoryStore())
}

func NewSharedProvider(origin string, params Params, signingKeyPEM []byte, store *PostgresCredentials) (*Provider, error) {
	signer, err := NewSignerFromPEM(origin, signingKeyPEM)
	if err != nil {
		return nil, err
	}
	return newProvider(origin, time.Now, params, signer, store)
}

func newProvider(origin string, now func() time.Time, params Params, signer *Signer, store credentialStore) (*Provider, error) {
	if now == nil {
		now = time.Now
	}
	params = params.normalized()
	dummy, err := HashPassword("dummy-password-value", params)
	if err != nil {
		return nil, err
	}
	return &Provider{
		store:      store,
		signer:     signer,
		throttle:   NewThrottle(5, time.Minute),
		params:     params,
		dummy:      dummy,
		now:        now,
		origin:     origin,
		accessTTL:  10 * time.Minute,
		refreshTTL: 24 * time.Hour,
		codeTTL:    time.Minute,
		sessionTTL: 12 * time.Hour,
	}, nil
}

func (p *Provider) CreateUser(email, password, tenantID, role string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || tenantID == "" {
		return ErrInvalid
	}
	if role != platform.RoleOwner && role != platform.RoleMember && role != platform.RoleAgent {
		return ErrInvalid
	}
	hashed, err := HashPassword(password, p.params)
	if err != nil {
		return ErrInvalid
	}
	id, err := randomToken(16)
	if err != nil {
		return err
	}
	if !p.store.putUser(userRow{ID: id, Email: email, Password: hashed, TenantID: tenantID, Role: role}) {
		return ErrInvalid
	}
	return nil
}

func (p *Provider) RegisterClient(id, audience string, redirects []string) error {
	if id == "" || audience == "" || len(redirects) == 0 {
		return ErrInvalid
	}
	allowed := map[string]struct{}{}
	for _, redirect := range redirects {
		if redirect == "" || strings.Contains(redirect, "*") {
			return ErrInvalid
		}
		allowed[redirect] = struct{}{}
	}
	return p.store.putClient(clientRow{ID: id, Audience: audience, Redirects: allowed})
}

func (p *Provider) Login(email, password, ip string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	key := ip + "\x00" + email
	now := p.now()
	if !p.throttle.Allow(key, now) {
		return "", ErrRateLimited
	}
	user, ok := p.store.userByEmail(email)
	valid := false
	if ok {
		valid = VerifyPassword(user.Password, password)
	} else {
		_ = VerifyPassword(p.dummy, password)
	}
	if !valid {
		p.throttle.Fail(key, now)
		return "", ErrUnauthenticated
	}
	p.throttle.Clear(key)
	sessionID, err := randomToken(32)
	if err != nil {
		return "", err
	}
	if err = p.store.putSession(sessionRow{ID: sessionID, UserID: user.ID, Expires: now.Add(p.sessionTTL)}); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (p *Provider) Logout(sessionID string) error {
	row, ok := p.store.session(sessionID)
	if !ok {
		return nil
	}
	if err := p.store.deleteSession(sessionID); err != nil {
		return err
	}
	return p.store.revokeUserRefresh(row.UserID)
}

func (p *Provider) Authorize(sessionID, clientID, redirect, state, challenge, method, nonce string) (string, error) {
	now := p.now()
	session, ok := p.store.session(sessionID)
	if !ok || !session.Expires.After(now) {
		return "", ErrUnauthenticated
	}
	if _, ok = p.store.userByID(session.UserID); !ok {
		return "", ErrUnauthenticated
	}
	client, ok := p.store.client(clientID)
	if !ok {
		return "", ErrInvalid
	}
	if _, ok = client.Redirects[redirect]; !ok || state == "" || nonce == "" || method != "S256" || len(challenge) < 43 {
		return "", ErrInvalid
	}
	code, err := randomToken(32)
	if err != nil {
		return "", err
	}
	err = p.store.putCode(codeRow{
		Code: code, UserID: session.UserID, ClientID: clientID, Redirect: redirect,
		Challenge: challenge, Nonce: nonce, Expires: now.Add(p.codeTTL),
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

func (p *Provider) ExchangeCode(clientID, code, redirect, verifier string) (TokenSet, error) {
	row, ok := p.store.takeCode(code, p.now())
	if !ok || row.ClientID != clientID || row.Redirect != redirect || !VerifyPKCE(verifier, row.Challenge) {
		return TokenSet{}, ErrUnauthenticated
	}
	user, ok := p.store.userByID(row.UserID)
	if !ok {
		return TokenSet{}, ErrUnauthenticated
	}
	client, ok := p.store.client(clientID)
	if !ok {
		return TokenSet{}, ErrInvalid
	}
	return p.issue(user, client, "", row.Nonce)
}

func (p *Provider) Refresh(clientID, refreshToken string) (TokenSet, error) {
	hash := tokenHash(refreshToken)
	row, ok := p.store.refreshByHash(hash)
	if !ok || !row.Expires.After(p.now()) || row.ClientID != clientID {
		return TokenSet{}, ErrUnauthenticated
	}
	marked, markedOK := p.store.markRefreshUsed(hash)
	if !markedOK {
		if marked.Family != "" {
			_ = p.store.revokeFamily(marked.Family)
		}
		return TokenSet{}, ErrUnauthenticated
	}
	row = marked
	user, ok := p.store.userByID(row.UserID)
	if !ok {
		return TokenSet{}, ErrUnauthenticated
	}
	client, ok := p.store.client(clientID)
	if !ok {
		return TokenSet{}, ErrInvalid
	}
	return p.issue(user, client, row.Family, "")
}

func (p *Provider) issue(user userRow, client clientRow, family, nonce string) (TokenSet, error) {
	now := p.now()
	access, err := p.signer.Sign(user.ID, user.TenantID, user.Role, client.Audience, "", now, now.Add(p.accessTTL))
	if err != nil {
		return TokenSet{}, err
	}
	idToken, err := p.signer.Sign(user.ID, user.TenantID, user.Role, client.ID, nonce, now, now.Add(p.accessTTL))
	if err != nil {
		return TokenSet{}, err
	}
	if family == "" {
		family, err = randomToken(16)
		if err != nil {
			return TokenSet{}, err
		}
	}
	refresh, err := randomToken(32)
	if err != nil {
		return TokenSet{}, err
	}
	err = p.store.putRefresh(refreshRow{
		Hash: tokenHash(refresh), Family: family, UserID: user.ID, ClientID: client.ID,
		Expires: now.Add(p.refreshTTL),
	})
	if err != nil {
		return TokenSet{}, err
	}
	return TokenSet{AccessToken: access, RefreshToken: refresh, IDToken: idToken, ExpiresIn: int(p.accessTTL.Seconds()), TokenType: "Bearer"}, nil
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
