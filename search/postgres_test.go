package search

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"systemsfoundry/platform"
)

func postgresRepo(t *testing.T) (*PostgresRepo, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("FOUNDRY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("FOUNDRY_TEST_POSTGRES_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewPostgresRepo(pool, nil), pool
}

func freshTenant(t *testing.T, prefix string) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

func owner(tenantID string) platform.Actor {
	return platform.Actor{Subject: "owner-" + tenantID, TenantID: tenantID, Role: platform.RoleOwner}
}

func TestPostgresKeepsTenantsApartWithSameDocumentID(t *testing.T) {
	repo, _ := postgresRepo(t)
	ctx := context.Background()
	acme, beta := owner(freshTenant(t, "acme")), owner(freshTenant(t, "beta"))
	svc := NewService(repo)
	for _, step := range []struct {
		actor           platform.Actor
		id, title, body string
	}{
		{acme, "shared", "dogs", "red dogs run"},
		{beta, "shared", "cats", "blue cats sleep"},
		{beta, "beta-only", "dogs", "beta dogs secret"},
	} {
		if outcome := svc.Index(ctx, step.actor, step.id, step.title, step.body); !outcome.OK {
			t.Fatalf("index %s: %+v", step.id, outcome.Error)
		}
	}
	found := svc.Find(ctx, acme, "dogs", "", 50)
	if !found.OK || !slices.Equal(found.Value.IDs, []string{"shared"}) {
		t.Fatalf("acme ids = %+v", found)
	}
	lexical, _, err := repo.Rank(ctx, beta, []string{"dogs"}, platform.HashEmbed("dogs"), 10)
	if err != nil || !slices.Equal(lexical, []string{"beta-only"}) {
		t.Fatalf("beta lexical = %v err=%v", lexical, err)
	}
}

func TestPostgresAgentRoleCannotWriteEvenWhenServiceIsBypassed(t *testing.T) {
	repo, _ := postgresRepo(t)
	tenantID := freshTenant(t, "agent")
	agent := platform.Actor{Subject: "agent-1", TenantID: tenantID, Role: platform.RoleAgent}
	err := repo.Save(context.Background(), agent, Analyze("d1", "dogs", "red dogs"), platform.HashEmbed("dogs"))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("want permission denied (42501), got %v", err)
	}
	if _, _, err = repo.Rank(context.Background(), agent, []string{"dogs"}, platform.HashEmbed("dogs"), 10); err != nil {
		t.Fatalf("agent read failed: %v", err)
	}
}

func TestPostgresWithoutTenantSettingSeesNoRows(t *testing.T) {
	repo, pool := postgresRepo(t)
	ctx := context.Background()
	acme := owner(freshTenant(t, "acme"))
	if err := repo.Save(ctx, acme, Analyze("d1", "dogs", "red dogs"), platform.HashEmbed("dogs")); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, asWriterSQL); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM documents").Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("visible without tenant = %d", visible)
	}
}

func TestPostgresLexicalOrderMatchesMemoryBM25(t *testing.T) {
	repo, _ := postgresRepo(t)
	ctx := context.Background()
	actor := owner(freshTenant(t, "parity"))
	memory := NewMemoryRepo(100)
	corpus := [][3]string{
		{"a", "postgres rls", "row level security keeps tenants apart in postgres"},
		{"b", "vector search", "pgvector stores embeddings for semantic search"},
		{"c", "hybrid", "hybrid search fuses bm25 and vector search with rrf"},
		{"d", "unrelated", "kafka consumers and dead letter queues"},
	}
	for _, row := range corpus {
		doc := Analyze(row[0], row[1], row[2])
		vector := platform.HashEmbed(row[1] + " " + row[2])
		if err := repo.Save(ctx, actor, doc, vector); err != nil {
			t.Fatal(err)
		}
		if err := memory.Save(ctx, actor, doc, vector); err != nil {
			t.Fatal(err)
		}
	}
	terms := Tokens("search postgres")
	vector := platform.HashEmbed("search postgres")
	wantLexical, wantSemantic, _ := memory.Rank(ctx, actor, terms, vector, 10)
	gotLexical, gotSemantic, err := repo.Rank(ctx, actor, terms, vector, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotLexical, wantLexical) || !slices.Equal(gotSemantic, wantSemantic) {
		t.Fatalf("lexical got %v want %v; semantic got %v want %v", gotLexical, wantLexical, gotSemantic, wantSemantic)
	}
}
