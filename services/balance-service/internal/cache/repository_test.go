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
	require.NoError(t, server.Set("account:acct_demo", string(payload)))

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
