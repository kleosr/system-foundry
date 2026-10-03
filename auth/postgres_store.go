package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	authRoleSQL       = "SET LOCAL ROLE auth_service"
	authStatementCap  = "SET LOCAL statement_timeout = 2000"
	insertUserSQL     = "INSERT INTO users (user_id, email, password_hash, tenant_id, role) VALUES ($1, $2, $3, $4, $5)"
	userByEmailSQL    = "SELECT user_id, email, password_hash, tenant_id, role FROM users WHERE email = $1"
	userByIDSQL       = "SELECT user_id, email, password_hash, tenant_id, role FROM users WHERE user_id = $1"
	insertSessionSQL  = "INSERT INTO sessions (session_hash, user_id, expires_at) VALUES ($1, $2, $3)"
	sessionSQL        = "SELECT user_id, expires_at FROM sessions WHERE session_hash = $1"
	deleteSessionSQL  = "DELETE FROM sessions WHERE session_hash = $1"
	upsertClientSQL   = "INSERT INTO oauth_clients (client_id, audience, redirects) VALUES ($1, $2, $3) ON CONFLICT (client_id) DO UPDATE SET audience = EXCLUDED.audience, redirects = EXCLUDED.redirects"
	clientSQL         = "SELECT audience, redirects FROM oauth_clients WHERE client_id = $1"
	insertCodeSQL     = "INSERT INTO auth_codes (code_hash, user_id, client_id, redirect_uri, challenge, nonce, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7)"
	takeCodeSQL       = "UPDATE auth_codes SET used = true WHERE code_hash = $1 AND NOT used AND expires_at > $2 RETURNING user_id, client_id, redirect_uri, challenge, nonce, expires_at"
	insertRefreshSQL  = "INSERT INTO refresh_tokens (token_hash, family, user_id, client_id, expires_at) VALUES ($1, $2, $3, $4, $5)"
	refreshSQL        = "SELECT family, user_id, client_id, expires_at, used FROM refresh_tokens WHERE token_hash = $1"
	markRefreshSQL    = "UPDATE refresh_tokens SET used = true WHERE token_hash = $1 AND NOT used RETURNING family, user_id, client_id, expires_at, used"
	revokeFamilySQL   = "DELETE FROM refresh_tokens WHERE family = $1"
	revokeUserRefresh = "DELETE FROM refresh_tokens WHERE user_id = $1"
)

type PostgresCredentials struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewPostgresCredentials(pool *pgxpool.Pool, logger *slog.Logger) *PostgresCredentials {
	return &PostgresCredentials{pool: pool, logger: logger}
}

func (s *PostgresCredentials) inTx(work func(ctx context.Context, tx pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, setup := range []string{authRoleSQL, authStatementCap} {
		if _, err = tx.Exec(ctx, setup); err != nil {
			return err
		}
	}
	if err = work(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresCredentials) exec(statement string, args ...any) error {
	return s.inTx(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, statement, args...)
		return err
	})
}

func (s *PostgresCredentials) found(err error) bool {
	if err == nil {
		return true
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		s.logger.Error("auth", "event", "credential_store_failed", "message", err.Error())
	}
	return false
}

func (s *PostgresCredentials) putUser(row userRow) bool {
	err := s.exec(insertUserSQL, row.ID, row.Email, row.Password, row.TenantID, row.Role)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return false
	}
	return s.found(err)
}

func (s *PostgresCredentials) scanUser(statement, key string) (userRow, bool) {
	var row userRow
	err := s.inTx(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, statement, key).Scan(&row.ID, &row.Email, &row.Password, &row.TenantID, &row.Role)
	})
	return row, s.found(err)
}

func (s *PostgresCredentials) userByEmail(email string) (userRow, bool) {
	return s.scanUser(userByEmailSQL, email)
}

func (s *PostgresCredentials) userByID(id string) (userRow, bool) {
	return s.scanUser(userByIDSQL, id)
}

func (s *PostgresCredentials) putSession(row sessionRow) error {
	return s.exec(insertSessionSQL, tokenHash(row.ID), row.UserID, row.Expires)
}

func (s *PostgresCredentials) session(id string) (sessionRow, bool) {
	row := sessionRow{ID: id}
	err := s.inTx(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sessionSQL, tokenHash(id)).Scan(&row.UserID, &row.Expires)
	})
	return row, s.found(err)
}

func (s *PostgresCredentials) deleteSession(id string) error {
	return s.exec(deleteSessionSQL, tokenHash(id))
}

func (s *PostgresCredentials) putClient(row clientRow) error {
	redirects := make([]string, 0, len(row.Redirects))
	for redirect := range row.Redirects {
		redirects = append(redirects, redirect)
	}
	return s.exec(upsertClientSQL, row.ID, row.Audience, redirects)
}

func (s *PostgresCredentials) client(id string) (clientRow, bool) {
	row := clientRow{ID: id, Redirects: map[string]struct{}{}}
	var redirects []string
	err := s.inTx(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, clientSQL, id).Scan(&row.Audience, &redirects)
	})
	for _, redirect := range redirects {
		row.Redirects[redirect] = struct{}{}
	}
	return row, s.found(err)
}

func (s *PostgresCredentials) putCode(row codeRow) error {
	return s.exec(insertCodeSQL, tokenHash(row.Code), row.UserID, row.ClientID, row.Redirect, row.Challenge, row.Nonce, row.Expires)
}

func (s *PostgresCredentials) takeCode(code string, now time.Time) (codeRow, bool) {
	row := codeRow{Code: code, Used: true}
	err := s.inTx(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, takeCodeSQL, tokenHash(code), now).Scan(&row.UserID, &row.ClientID, &row.Redirect, &row.Challenge, &row.Nonce, &row.Expires)
	})
	return row, s.found(err)
}

func (s *PostgresCredentials) putRefresh(row refreshRow) error {
	return s.exec(insertRefreshSQL, row.Hash, row.Family, row.UserID, row.ClientID, row.Expires)
}

func (s *PostgresCredentials) scanRefresh(statement, hash string) (refreshRow, bool) {
	row := refreshRow{Hash: hash}
	err := s.inTx(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, statement, hash).Scan(&row.Family, &row.UserID, &row.ClientID, &row.Expires, &row.Used)
	})
	return row, s.found(err)
}

func (s *PostgresCredentials) refreshByHash(hash string) (refreshRow, bool) {
	return s.scanRefresh(refreshSQL, hash)
}

func (s *PostgresCredentials) markRefreshUsed(hash string) (refreshRow, bool) {
	if row, ok := s.scanRefresh(markRefreshSQL, hash); ok {
		return row, true
	}
	row, _ := s.scanRefresh(refreshSQL, hash)
	return row, false
}

func (s *PostgresCredentials) revokeFamily(family string) error {
	return s.exec(revokeFamilySQL, family)
}

func (s *PostgresCredentials) revokeUserRefresh(userID string) error {
	return s.exec(revokeUserRefresh, userID)
}
