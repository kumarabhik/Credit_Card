package hold

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/cache"
	"github.com/redis/go-redis/v9"
)

var (
	// ErrInsufficientFunds is returned when an authorize request exceeds the available balance.
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrHoldNotFound is returned when a release or capture references a missing hold.
	ErrHoldNotFound = errors.New("hold not found")
)

var holdScript = redis.NewScript(`
local meta_key = KEYS[1]
local snapshot_key = KEYS[2]
local holds_key = KEYS[3]
local hold_id = ARGV[1]
local amount = tonumber(ARGV[2])

local account_id = redis.call('HGET', meta_key, 'account_id')
if not account_id then
  return {'MISSING'}
end

local currency = redis.call('HGET', meta_key, 'currency')
local available = tonumber(redis.call('HGET', meta_key, 'available_minor') or '0')
local posted = tonumber(redis.call('HGET', meta_key, 'posted_minor') or '0')
local status = redis.call('HGET', meta_key, 'account_status') or 'ACTIVE'
local existing = redis.call('HGET', holds_key, hold_id)

if existing then
  return {'DUPLICATE', tostring(available), tostring(posted), status, currency}
end

if status ~= 'ACTIVE' or available < amount then
  return {'DECLINE', tostring(available), tostring(posted), status, currency}
end

local next_available = available - amount
redis.call('HSET', meta_key, 'available_minor', tostring(next_available))
redis.call('HSET', holds_key, hold_id, tostring(amount))
local snapshot = '{"id":"' .. account_id .. '","currency":"' .. currency .. '","available_minor":' .. next_available .. ',"posted_minor":' .. posted .. ',"account_status":"' .. status .. '"}'
redis.call('SET', snapshot_key, snapshot)
return {'APPROVE', tostring(next_available), tostring(posted), status, currency}
`)

var releaseScript = redis.NewScript(`
local meta_key = KEYS[1]
local snapshot_key = KEYS[2]
local holds_key = KEYS[3]
local hold_id = ARGV[1]

local account_id = redis.call('HGET', meta_key, 'account_id')
if not account_id then
  return {'MISSING'}
end

local amount = tonumber(redis.call('HGET', holds_key, hold_id) or '')
if not amount then
  return {'NOT_FOUND'}
end

local currency = redis.call('HGET', meta_key, 'currency')
local available = tonumber(redis.call('HGET', meta_key, 'available_minor') or '0')
local posted = tonumber(redis.call('HGET', meta_key, 'posted_minor') or '0')
local status = redis.call('HGET', meta_key, 'account_status') or 'ACTIVE'
local next_available = available + amount

redis.call('HSET', meta_key, 'available_minor', tostring(next_available))
redis.call('HDEL', holds_key, hold_id)
local snapshot = '{"id":"' .. account_id .. '","currency":"' .. currency .. '","available_minor":' .. next_available .. ',"posted_minor":' .. posted .. ',"account_status":"' .. status .. '"}'
redis.call('SET', snapshot_key, snapshot)
return {'RELEASED', tostring(next_available), tostring(posted), status, currency}
`)

var captureScript = redis.NewScript(`
local meta_key = KEYS[1]
local snapshot_key = KEYS[2]
local holds_key = KEYS[3]
local hold_id = ARGV[1]

local account_id = redis.call('HGET', meta_key, 'account_id')
if not account_id then
  return {'MISSING'}
end

local amount = tonumber(redis.call('HGET', holds_key, hold_id) or '')
if not amount then
  return {'NOT_FOUND'}
end

local currency = redis.call('HGET', meta_key, 'currency')
local available = tonumber(redis.call('HGET', meta_key, 'available_minor') or '0')
local posted = tonumber(redis.call('HGET', meta_key, 'posted_minor') or '0')
local status = redis.call('HGET', meta_key, 'account_status') or 'ACTIVE'
local next_posted = posted + amount

redis.call('HSET', meta_key, 'posted_minor', tostring(next_posted))
redis.call('HDEL', holds_key, hold_id)
local snapshot = '{"id":"' .. account_id .. '","currency":"' .. currency .. '","available_minor":' .. available .. ',"posted_minor":' .. next_posted .. ',"account_status":"' .. status .. '"}'
redis.call('SET', snapshot_key, snapshot)
return {'CAPTURED', tostring(available), tostring(next_posted), status, currency}
`)

// Manager applies account mutations atomically inside Redis Lua scripts.
type Manager struct {
	client redis.Cmdable
}

// NewManager returns a hold manager backed by Redis scripts.
func NewManager(client redis.Cmdable) *Manager {
	return &Manager{client: client}
}

// Hold places a temporary reservation against the available balance.
func (m *Manager) Hold(ctx context.Context, accountID, holdID string, amountMinor int64) (*account.Snapshot, error) {
	values, err := holdScript.Run(ctx, m.client, keys(accountID), holdID, amountMinor).Result()
	if err != nil {
		return nil, fmt.Errorf("run hold script for account %s: %w", accountID, err)
	}
	return parseMutationResult(accountID, values)
}

// Release removes a temporary hold and restores the available balance.
func (m *Manager) Release(ctx context.Context, accountID, holdID string) (*account.Snapshot, error) {
	values, err := releaseScript.Run(ctx, m.client, keys(accountID), holdID).Result()
	if err != nil {
		return nil, fmt.Errorf("run release script for account %s: %w", accountID, err)
	}
	return parseMutationResult(accountID, values)
}

// Capture finalizes a hold and moves its amount into the posted balance.
func (m *Manager) Capture(ctx context.Context, accountID, holdID string) (*account.Snapshot, error) {
	values, err := captureScript.Run(ctx, m.client, keys(accountID), holdID).Result()
	if err != nil {
		return nil, fmt.Errorf("run capture script for account %s: %w", accountID, err)
	}
	return parseMutationResult(accountID, values)
}

func keys(accountID string) []string {
	return []string{
		fmt.Sprintf("account-meta:{%s}", accountID),
		fmt.Sprintf("account:{%s}", accountID),
		fmt.Sprintf("account-holds:{%s}", accountID),
	}
}

func parseMutationResult(accountID string, value any) (*account.Snapshot, error) {
	entries, ok := value.([]any)
	if !ok || len(entries) < 1 {
		return nil, fmt.Errorf("unexpected lua response for account %s: %T", accountID, value)
	}

	status := stringify(entries[0])
	switch status {
	case "APPROVE", "RELEASED", "CAPTURED", "DUPLICATE":
	case "DECLINE":
		return nil, ErrInsufficientFunds
	case "NOT_FOUND":
		return nil, ErrHoldNotFound
	case "MISSING":
		return nil, cache.ErrAccountNotFound
	default:
		return nil, fmt.Errorf("unknown lua status %q for account %s", status, accountID)
	}

	snapshot := &account.Snapshot{ID: accountID}
	if len(entries) > 1 {
		available, err := strconv.ParseInt(stringify(entries[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse available balance for account %s: %w", accountID, err)
		}
		snapshot.AvailableMinor = available
	}
	if len(entries) > 2 {
		posted, err := strconv.ParseInt(stringify(entries[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse posted balance for account %s: %w", accountID, err)
		}
		snapshot.PostedMinor = posted
	}
	if len(entries) > 3 {
		snapshot.AccountStatus = stringify(entries[3])
	}
	if len(entries) > 4 {
		snapshot.Currency = stringify(entries[4])
	}
	return snapshot, nil
}

func stringify(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}
