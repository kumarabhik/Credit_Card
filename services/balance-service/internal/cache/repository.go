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
	key := fmt.Sprintf("account:%s", accountID)
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
