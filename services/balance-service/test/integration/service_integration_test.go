//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/cache"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/hold"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/obs"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/service"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/store"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	pgmodule "github.com/testcontainers/testcontainers-go/modules/postgres"
	redismodule "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestAccountServiceHydratesCacheAndAppliesHoldLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	pgContainer, err := pgmodule.Run(
		ctx,
		"postgres:16-alpine",
		pgmodule.WithDatabase("cc"),
		pgmodule.WithUsername("postgres"),
		pgmodule.WithPassword("postgres"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testcontainers.TerminateContainer(pgContainer)) })

	redisContainer, err := redismodule.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testcontainers.TerminateContainer(redisContainer)) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := store.OpenDB(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	_, err = db.ExecContext(ctx, `
CREATE TABLE accounts (
  account_id TEXT PRIMARY KEY,
  currency TEXT NOT NULL,
  available_minor BIGINT NOT NULL,
  posted_minor BIGINT NOT NULL,
  account_status TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO accounts (account_id, currency, available_minor, posted_minor, account_status)
VALUES ('acct_demo_card', 'USD', 10000, 1000, 'ACTIVE');
`)
	require.NoError(t, err)

	redisAddr, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: redisAddr})

	logger, err := obs.NewLogger("balance-service-test")
	require.NoError(t, err)

	service := service.New(
		cache.NewRepository(client),
		store.NewPostgresRepository(db),
		hold.NewManager(client),
		logger,
	)

	result, err := service.Authorize(ctx, account.AuthorizationRequest{
		AccountID: "acct_demo_card",
		TxnID:     "txn_hold_1",
		Currency:  "USD",
		MinorUnit: 250,
	})
	require.NoError(t, err)
	require.True(t, result.Approved)
	require.False(t, result.CacheHit)
	require.Equal(t, int64(9750), result.Snapshot.AvailableMinor)

	cached, err := cache.NewRepository(client).Lookup(ctx, "acct_demo_card")
	require.NoError(t, err)
	require.Equal(t, int64(9750), cached.AvailableMinor)

	released, err := service.Release(ctx, "acct_demo_card", "txn_hold_1")
	require.NoError(t, err)
	require.Equal(t, int64(10000), released.Snapshot.AvailableMinor)

	second, err := service.Authorize(ctx, account.AuthorizationRequest{
		AccountID: "acct_demo_card",
		TxnID:     "txn_hold_2",
		Currency:  "USD",
		MinorUnit: 500,
	})
	require.NoError(t, err)
	require.True(t, second.CacheHit)

	captured, err := service.Capture(ctx, "acct_demo_card", "txn_hold_2")
	require.NoError(t, err)
	require.Equal(t, int64(9500), captured.Snapshot.AvailableMinor)
	require.Equal(t, int64(1500), captured.Snapshot.PostedMinor)

	dbSnapshot, err := store.NewPostgresRepository(db).GetAccount(ctx, "acct_demo_card")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%+v", captured.Snapshot), fmt.Sprintf("%+v", dbSnapshot))
}
