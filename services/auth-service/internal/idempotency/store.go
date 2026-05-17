package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrConflict indicates the idempotency key has been reused with a different request body.
	ErrConflict = errors.New("idempotency key conflict")
)

// ClaimStatus describes the state of an idempotency lookup attempt.
type ClaimStatus string

const (
	// StatusLeader means this request won the claim and should execute business logic.
	StatusLeader ClaimStatus = "LEADER"
	// StatusReplay means a cached response already exists and should be replayed.
	StatusReplay ClaimStatus = "REPLAY"
)

// ClaimResult is returned by a Store when claiming an idempotency key.
type ClaimResult struct {
	Status   ClaimStatus
	Response *authv1.AuthorizeResponse
}

// Store handles request claim, replay, and completion for authorize responses.
type Store interface {
	ClaimOrReplay(ctx context.Context, key string, request *authv1.AuthorizeRequest, ttl time.Duration) (ClaimResult, error)
	Complete(ctx context.Context, key string, request *authv1.AuthorizeRequest, response *authv1.AuthorizeResponse, ttl time.Duration) error
	Abandon(ctx context.Context, key string) error
	Ready(ctx context.Context) error
}

// RequestHash creates a deterministic body hash for idempotency matching.
func RequestHash(request *authv1.AuthorizeRequest) (string, error) {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func marshalResponse(response *authv1.AuthorizeResponse) (string, error) {
	payload, err := protojson.Marshal(response)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}

func unmarshalResponse(encoded string) (*authv1.AuthorizeResponse, error) {
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	response := new(authv1.AuthorizeResponse)
	if err := protojson.Unmarshal(payload, response); err != nil {
		return nil, err
	}
	return response, nil
}
