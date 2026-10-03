package auth

import (
	"errors"
	"testing"
	"time"
)

const verifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func testProvider(t *testing.T) (*Provider, *time.Time) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	provider, err := NewProvider("https://foundry.local", func() time.Time { return now }, Params{Time: 1, Memory: 8 * 1024, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return provider, &now
}

func seedOwner(t *testing.T, provider *Provider) {
	t.Helper()
	if err := provider.CreateUser("ada@example.com", "correct-horse", "acme", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := provider.RegisterClient("cli", "foundry-api", []string{"https://app.example/callback"}); err != nil {
		t.Fatal(err)
	}
}

func tokensFor(t *testing.T, provider *Provider) TokenSet {
	t.Helper()
	sessionID, err := provider.Login("ada@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, err := provider.Authorize(sessionID, "cli", "https://app.example/callback", "state-1", challengeFor(verifier), "S256", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	set, err := provider.ExchangeCode("cli", code, "https://app.example/callback", verifier)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestBadPasswordAndMissingUserMatch(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	_, missing := provider.Login("nope@example.com", "correct-horse", "127.0.0.1")
	_, wrong := provider.Login("ada@example.com", "wrong-password", "127.0.0.1")
	if !errors.Is(missing, ErrUnauthenticated) || !errors.Is(wrong, ErrUnauthenticated) || missing.Error() != wrong.Error() {
		t.Fatalf("missing=%v wrong=%v", missing, wrong)
	}
}

func TestLoginRateLimit(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	for i := 0; i < 5; i++ {
		_, err := provider.Login("ada@example.com", "wrong-password", "10.0.0.8")
		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	_, err := provider.Login("ada@example.com", "correct-horse", "10.0.0.8")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	first := tokensFor(t, provider)
	second, err := provider.Refresh("cli", first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Refresh("cli", first.RefreshToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err = provider.Refresh("cli", second.RefreshToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestExpiredAndWrongAudienceTokensFail(t *testing.T) {
	provider, now := testProvider(t)
	seedOwner(t, provider)
	set := tokensFor(t, provider)
	if _, err := provider.signer.Verify(set.AccessToken, "other-api", *now); err == nil {
		t.Fatal("wrong audience accepted")
	}
	*now = now.Add(11 * time.Minute)
	if _, err := provider.signer.Verify(set.AccessToken, "foundry-api", *now); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestNoneAlgorithmRejected(t *testing.T) {
	provider, now := testProvider(t)
	_, err := provider.signer.Verify("eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0.", "foundry-api", *now)
	if err == nil {
		t.Fatal("none alg accepted")
	}
}

func TestCodeReplayFails(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	sessionID, err := provider.Login("ada@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, err := provider.Authorize(sessionID, "cli", "https://app.example/callback", "state-1", challengeFor(verifier), "S256", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.ExchangeCode("cli", code, "https://app.example/callback", verifier); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.ExchangeCode("cli", code, "https://app.example/callback", verifier); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestLogoutRevokesRefresh(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	sessionID, err := provider.Login("ada@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, err := provider.Authorize(sessionID, "cli", "https://app.example/callback", "state-1", challengeFor(verifier), "S256", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	set, err := provider.ExchangeCode("cli", code, "https://app.example/callback", verifier)
	if err != nil {
		t.Fatal(err)
	}
	provider.Logout(sessionID)
	if _, err = provider.Refresh("cli", set.RefreshToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestOpenRedirectRejected(t *testing.T) {
	provider, _ := testProvider(t)
	seedOwner(t, provider)
	sessionID, err := provider.Login("ada@example.com", "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Authorize(sessionID, "cli", "https://evil.example/callback", "state-1", challengeFor(verifier), "S256", "nonce-1")
	if !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
