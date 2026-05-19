package cache

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestLookupReturnsHotPathAccountSnapshot(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	repository := NewRepository(client)

	expected := &account.Snapshot{
		ID:             "acct_demo",
		Currency:       "USD",
		AvailableMinor: 9000,
		PostedMinor:    1000,
		AccountStatus:  "ACTIVE",
	}
	payload, err := json.Marshal(expected)
	require.NoError(t, err)
	require.NoError(t, server.Set("account:{acct_demo}", string(payload)))

	actual, err := repository.Lookup(context.Background(), "acct_demo")
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestLookupMissReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	repository := NewRepository(client)

	_, err := repository.Lookup(context.Background(), "missing")
	require.ErrorIs(t, err, ErrAccountNotFound)
}

func TestHydrateWritesSnapshotAndMeta(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	repository := NewRepository(client)

	snapshot := &account.Snapshot{
		ID:             "acct_demo",
		Currency:       "USD",
		AvailableMinor: 1200,
		PostedMinor:    300,
		AccountStatus:  "ACTIVE",
	}

	err := repository.Hydrate(context.Background(), snapshot)
	require.NoError(t, err)

	payload, err := server.Get("account:{acct_demo}")
	require.NoError(t, err)
	require.Contains(t, payload, "\"available_minor\":1200")

	currency := server.HGet("account-meta:{acct_demo}", "currency")
	available := server.HGet("account-meta:{acct_demo}", "available_minor")
	require.Equal(t, "USD", currency)
	require.Equal(t, "1200", available)
}
