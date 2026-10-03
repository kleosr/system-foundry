package auth

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
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func sharedProvider(t *testing.T, pool *pgxpool.Pool, keyPEM []byte) *Provider {
	t.Helper()
	provider, err := NewSharedProvider("https://foundry.local", Params{Time: 1, Memory: 8 * 1024, Threads: 1}, keyPEM,
		NewPostgresCredentials(pool, slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testSigningKey(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestPostgresCredentialsSurviveRestartAndDetectRefreshReuse(t *testing.T) {
	dsn := os.Getenv("FOUNDRY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("FOUNDRY_TEST_POSTGRES_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	keyPEM := testSigningKey(t)
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	email := "ada-" + hex.EncodeToString(suffix) + "@example.com"
	first := sharedProvider(t, pool, keyPEM)
	if err = first.CreateUser(email, "correct-horse", "acme", "owner"); err != nil {
		t.Fatal(err)
	}
	if err = first.CreateUser(email, "correct-horse", "acme", "owner"); err == nil {
		t.Fatal("duplicate email accepted")
	}
	if err = first.RegisterClient("cli", "foundry-api", []string{"https://app.example/callback"}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := first.Login(email, "correct-horse", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	code, err := first.Authorize(sessionID, "cli", "https://app.example/callback", "s", challengeFor(verifier), "S256", "n")
	if err != nil {
		t.Fatal(err)
	}
	set, err := first.ExchangeCode("cli", code, "https://app.example/callback", verifier)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.ExchangeCode("cli", code, "https://app.example/callback", verifier); err == nil {
		t.Fatal("code redeemed twice")
	}

	restarted := sharedProvider(t, pool, keyPEM)
	if _, err = restarted.signer.Verify(set.AccessToken, "foundry-api", restarted.now()); err != nil {
		t.Fatalf("token from before restart: %v", err)
	}
	if actor, ok := restarted.SessionActor(sessionID); !ok || actor.TenantID != "acme" {
		t.Fatalf("session after restart = %+v %v", actor, ok)
	}
	rotated, err := restarted.Refresh("cli", set.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Refresh("cli", set.RefreshToken); err == nil {
		t.Fatal("refresh token reused")
	}
	if _, err = restarted.Refresh("cli", rotated.RefreshToken); err == nil {
		t.Fatal("family survived reuse detection")
	}
	if err = restarted.Logout(sessionID); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.SessionActor(sessionID); ok {
		t.Fatal("session survived logout")
	}
}

func TestSignerRejectsNonP256Key(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	if _, err = NewSignerFromPEM("https://x", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err == nil {
		t.Fatal("P-384 key accepted for ES256")
	}
	if _, err = NewSignerFromPEM("https://x", []byte("not pem")); err == nil {
		t.Fatal("garbage accepted")
	}
}
