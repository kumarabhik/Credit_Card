package idempotency

import (
	"context"
	"fmt"
	"sync"
	"time"

	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
	"google.golang.org/protobuf/proto"
)

type memoryRecord struct {
	bodyHash     string
	response     *authv1.AuthorizeResponse
	completed    bool
	lastModified time.Time
	wait         *sync.Cond
}

// MemoryStore provides a concurrency-safe store for tests and local development.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]*memoryRecord
	now     func() time.Time
}

// NewMemoryStore returns an in-memory implementation of the idempotency store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string]*memoryRecord),
		now:     time.Now,
	}
}

// ClaimOrReplay claims a request or waits for the leader to finish.
func (s *MemoryStore) ClaimOrReplay(ctx context.Context, key string, request *authv1.AuthorizeRequest, _ time.Duration) (ClaimResult, error) {
	bodyHash, err := RequestHash(request)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("hash request for key %s: %w", key, err)
	}

	s.mu.Lock()
	record, ok := s.records[key]
	if !ok {
		record = &memoryRecord{bodyHash: bodyHash, wait: sync.NewCond(&s.mu)}
		s.records[key] = record
		s.mu.Unlock()
		return ClaimResult{Status: StatusLeader}, nil
	}

	if record.bodyHash != bodyHash {
		s.mu.Unlock()
		return ClaimResult{}, ErrConflict
	}

	for !record.completed {
		if ctx.Err() != nil {
			s.mu.Unlock()
			return ClaimResult{}, ctx.Err()
		}
		record.wait.Wait()
	}

	response := protoClone(record.response)
	s.mu.Unlock()
	return ClaimResult{Status: StatusReplay, Response: response}, nil
}

// Complete stores the cached response and wakes any waiters.
func (s *MemoryStore) Complete(_ context.Context, key string, request *authv1.AuthorizeRequest, response *authv1.AuthorizeResponse, _ time.Duration) error {
	bodyHash, err := RequestHash(request)
	if err != nil {
		return fmt.Errorf("hash request for key %s: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.records[key]
	if !ok {
		return fmt.Errorf("complete idempotency key %s: missing claim", key)
	}
	if record.bodyHash != bodyHash {
		return ErrConflict
	}

	record.completed = true
	record.response = protoClone(response)
	record.lastModified = s.now()
	record.wait.Broadcast()
	return nil
}

// Abandon removes a pending claim so a future request can retry cleanly.
func (s *MemoryStore) Abandon(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.records, key)
	return nil
}

// Ready always succeeds for the in-memory store.
func (s *MemoryStore) Ready(context.Context) error {
	return nil
}

func protoClone(response *authv1.AuthorizeResponse) *authv1.AuthorizeResponse {
	if response == nil {
		return nil
	}
	return proto.Clone(response).(*authv1.AuthorizeResponse)
}
