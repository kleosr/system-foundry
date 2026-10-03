package search

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

var ErrNoDatabaseRole = errors.New("actor has no database role")

const (
	asWriterSQL     = "SET LOCAL ROLE app_writer"
	asReaderSQL     = "SET LOCAL ROLE agent_reader"
	statementCapSQL = "SET LOCAL statement_timeout = 2000"
	vectorRecallSQL = "SET LOCAL hnsw.ef_search = 100"
	ensureTenantSQL = "INSERT INTO tenants (tenant_id) VALUES ($1) ON CONFLICT (tenant_id) DO NOTHING"
	clearTermsSQL   = "DELETE FROM document_terms WHERE tenant_id = $1 AND document_id = $2"
	insertTermsSQL  = "INSERT INTO document_terms (tenant_id, document_id, term, freq) SELECT $1, $2, t.term, t.freq FROM unnest($3::text[], $4::int[]) AS t(term, freq)"
	upsertDocument  = `INSERT INTO documents (tenant_id, document_id, title, body, embedding, term_count)
VALUES ($1, $2, $3, $4, $5::vector, $6)
ON CONFLICT (tenant_id, document_id) DO UPDATE
SET title = EXCLUDED.title, body = EXCLUDED.body, embedding = EXCLUDED.embedding,
    term_count = EXCLUDED.term_count, updated_at = now()`
	lexicalRankSQL = `WITH q AS (SELECT unnest($1::text[]) AS term),
stats AS (
    SELECT count(*)::float8 AS n,
           CASE WHEN coalesce(avg(term_count), 0) = 0 THEN 1 ELSE avg(term_count)::float8 END AS avgdl
    FROM documents
),
df AS (
    SELECT term, count(*)::float8 AS df FROM document_terms WHERE term = ANY($1::text[]) GROUP BY term
),
scored AS (
    SELECT t.document_id,
           sum(ln(1 + (s.n - df.df + 0.5) / (df.df + 0.5)) * (t.freq * 2.2) / (t.freq + 1.2 * (0.25 + 0.75 * greatest(d.term_count, 1) / s.avgdl))) AS score
    FROM q
    JOIN document_terms t ON t.term = q.term
    JOIN df ON df.term = q.term
    JOIN documents d ON d.tenant_id = t.tenant_id AND d.document_id = t.document_id
    CROSS JOIN stats s
    GROUP BY t.document_id
)
SELECT document_id FROM scored WHERE score > 0
ORDER BY score DESC, document_id COLLATE "C"
LIMIT $2`
	semanticRankSQL = `SELECT document_id FROM documents
ORDER BY embedding <=> $1::vector, document_id COLLATE "C"
LIMIT $2`
)

type QueryTracer interface {
	StartDB(ctx context.Context, statement string) (context.Context, func(error))
}

type PostgresRepo struct {
	pool   *pgxpool.Pool
	tracer QueryTracer
}

func NewPostgresRepo(pool *pgxpool.Pool, tracer QueryTracer) *PostgresRepo {
	return &PostgresRepo{pool: pool, tracer: tracer}
}

func (r *PostgresRepo) Save(ctx context.Context, actor platform.Actor, doc Document, vector []float32) error {
	terms := make([]string, 0, len(doc.Terms))
	freqs := make([]int32, 0, len(doc.Terms))
	for term, freq := range doc.Terms {
		terms = append(terms, term)
		freqs = append(freqs, int32(freq))
	}
	return r.inTenant(ctx, actor, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, ensureTenantSQL, actor.TenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, upsertDocument, actor.TenantID, doc.ID, doc.Title, doc.Body, vectorLiteral(vector), doc.Length); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, clearTermsSQL, actor.TenantID, doc.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, insertTermsSQL, actor.TenantID, doc.ID, terms, freqs)
		return err
	})
}

func (r *PostgresRepo) Rank(ctx context.Context, actor platform.Actor, terms []string, vector []float32, depth int) ([]string, []string, error) {
	var lexical, semantic []string
	err := r.inTenant(ctx, actor, func(tx pgx.Tx) error {
		var err error
		if lexical, err = collectIDs(ctx, tx, lexicalRankSQL, terms, depth); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, vectorRecallSQL); err != nil {
			return err
		}
		semantic, err = collectIDs(ctx, tx, semanticRankSQL, vectorLiteral(vector), depth)
		return err
	})
	return lexical, semantic, err
}

func (r *PostgresRepo) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *PostgresRepo) inTenant(ctx context.Context, actor platform.Actor, work func(pgx.Tx) error) (err error) {
	role, ok := auth.DatabaseRoleFor(actor)
	if !ok || actor.TenantID == "" {
		return ErrNoDatabaseRole
	}
	if r.tracer != nil {
		var finish func(error)
		ctx, finish = r.tracer.StartDB(ctx, "search tenant transaction as "+string(role))
		defer func() { finish(err) }()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	asRole := asWriterSQL
	if role == auth.DatabaseReader {
		asRole = asReaderSQL
	}
	for _, setup := range []string{asRole, statementCapSQL} {
		if _, err = tx.Exec(ctx, setup); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, SetTenantSQL, actor.TenantID); err != nil {
		return err
	}
	if err = work(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func collectIDs(ctx context.Context, tx pgx.Tx, statement string, first any, depth int) ([]string, error) {
	rows, err := tx.Query(ctx, statement, first, depth)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func vectorLiteral(vector []float32) string {
	var b strings.Builder
	b.Grow(len(vector) * 10)
	b.WriteByte('[')
	for i, v := range vector {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
