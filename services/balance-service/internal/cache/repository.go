package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
	"github.com/redis/go-redis/v9"
)

// ErrAccountNotFound is returned when the Redis hot-path cache misses.
var ErrAccountNotFound = errors.New("account not found")

// Repository reads account snapshots from Redis using the account:{id} key pattern.
type Repository struct {
	client redis.Cmdable
}

// NewRepository returns a cache repository backed by the provided Redis client.
func NewRepository(client redis.Cmdable) *Repository {
	return &Repository{client: client}
}

// Lookup loads the serialized account snapshot from Redis.
func (r *Repository) Lookup(ctx context.Context, accountID string) (*account.Snapshot, error) {
	key := snapshotKey(accountID)
	payload, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("lookup redis key %s: %w", key, err)
	}

	snapshot := new(account.Snapshot)
	if err := json.Unmarshal([]byte(payload), snapshot); err != nil {
		return nil, fmt.Errorf("decode redis payload for key %s: %w", key, err)
	}
	return snapshot, nil
}

// Hydrate writes the account snapshot to both the GET hot-path key and the script metadata hash.
func (r *Repository) Hydrate(ctx context.Context, snapshot *account.Snapshot) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal account snapshot %s: %w", snapshot.ID, err)
	}

	_, err = r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, snapshotKey(snapshot.ID), payload, 0)
		pipe.HSet(ctx, metaKey(snapshot.ID), map[string]any{
			"account_id":      snapshot.ID,
			"currency":        snapshot.Currency,
			"available_minor": snapshot.AvailableMinor,
			"posted_minor":    snapshot.PostedMinor,
			"account_status":  snapshot.AccountStatus,
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("hydrate account snapshot %s: %w", snapshot.ID, err)
	}
	return nil
}

// Ready verifies Redis is reachable.
func (r *Repository) Ready(ctx context.Context) error {
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}

func snapshotKey(accountID string) string {
	return fmt.Sprintf("account:{%s}", accountID)
}

func metaKey(accountID string) string {
	return fmt.Sprintf("account-meta:{%s}", accountID)
}
