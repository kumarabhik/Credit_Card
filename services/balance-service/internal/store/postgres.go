package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
)

// ErrAccountMissing is returned when an account does not exist in the recovery store.
var ErrAccountMissing = errors.New("account missing")

// PostgresRepository handles recovery-state reads and writes for account snapshots.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository returns a Postgres-backed account repository.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// OpenDB establishes the process-wide Postgres connection pool.
func OpenDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(10)
	return db, nil
}

// Ready confirms Postgres is reachable.
func (r *PostgresRepository) Ready(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

// GetAccount loads the authoritative account snapshot from Postgres.
func (r *PostgresRepository) GetAccount(ctx context.Context, accountID string) (*account.Snapshot, error) {
	const query = `
SELECT account_id, currency, available_minor, posted_minor, account_status
FROM accounts
WHERE account_id = $1
`
	snapshot := new(account.Snapshot)
	err := r.db.QueryRowContext(ctx, query, accountID).Scan(
		&snapshot.ID,
		&snapshot.Currency,
		&snapshot.AvailableMinor,
		&snapshot.PostedMinor,
		&snapshot.AccountStatus,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAccountMissing
		}
		return nil, fmt.Errorf("query account %s: %w", accountID, err)
	}
	return snapshot, nil
}

// UpsertAccount persists the latest account snapshot after a hold mutation.
func (r *PostgresRepository) UpsertAccount(ctx context.Context, snapshot *account.Snapshot) error {
	const statement = `
INSERT INTO accounts (account_id, currency, available_minor, posted_minor, account_status, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (account_id) DO UPDATE
SET currency = EXCLUDED.currency,
    available_minor = EXCLUDED.available_minor,
    posted_minor = EXCLUDED.posted_minor,
    account_status = EXCLUDED.account_status,
    updated_at = NOW()
`
	if _, err := r.db.ExecContext(
		ctx,
		statement,
		snapshot.ID,
		snapshot.Currency,
		snapshot.AvailableMinor,
		snapshot.PostedMinor,
		snapshot.AccountStatus,
	); err != nil {
		return fmt.Errorf("upsert account %s: %w", snapshot.ID, err)
	}
	return nil
}
