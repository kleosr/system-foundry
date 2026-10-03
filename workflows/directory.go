package workflows

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errTenantTransition = errors.New("tenant is missing or already in a final state")

const (
	directoryRoleSQL   = "SET LOCAL ROLE app_writer"
	directoryTenantSQL = "SELECT set_config('app.tenant_id', $1, true)"
	createTenantSQL    = "INSERT INTO tenants (tenant_id, status) VALUES ($1, 'pending') ON CONFLICT (tenant_id) DO NOTHING"
	activateTenantSQL  = "UPDATE tenants SET status = 'active' WHERE tenant_id = $1 AND status IN ('pending', 'active')"
	expireTenantSQL    = "UPDATE tenants SET status = 'expired' WHERE tenant_id = $1 AND status IN ('pending', 'expired')"
)

type PostgresDirectory struct {
	pool *pgxpool.Pool
}

func NewPostgresDirectory(pool *pgxpool.Pool) *PostgresDirectory {
	return &PostgresDirectory{pool: pool}
}

func (d *PostgresDirectory) CreateTenant(ctx context.Context, tenantID string) error {
	_, err := d.exec(ctx, tenantID, createTenantSQL)
	return err
}

func (d *PostgresDirectory) ProvisionTenant(ctx context.Context, tenantID string) error {
	return d.transition(ctx, tenantID, activateTenantSQL)
}

func (d *PostgresDirectory) ExpireTenant(ctx context.Context, tenantID string) error {
	return d.transition(ctx, tenantID, expireTenantSQL)
}

func (d *PostgresDirectory) transition(ctx context.Context, tenantID, statement string) error {
	changed, err := d.exec(ctx, tenantID, statement)
	if err != nil {
		return err
	}
	if changed == 0 {
		return errTenantTransition
	}
	return nil
}

func (d *PostgresDirectory) exec(ctx context.Context, tenantID, statement string) (int64, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, directoryRoleSQL); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, directoryTenantSQL, tenantID); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, statement, tenantID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}
