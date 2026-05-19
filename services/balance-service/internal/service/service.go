package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/cache"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/hold"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Service orchestrates cache lookup, Postgres recovery, and Redis hold mutations.
type Service struct {
	cache  *cache.Repository
	store  *store.PostgresRepository
	holds  *hold.Manager
	logger *zap.Logger
	tracer trace.Tracer
}

// New wires the balance service dependencies.
func New(cacheRepo *cache.Repository, storeRepo *store.PostgresRepository, holdManager *hold.Manager, logger *zap.Logger) *Service {
	return &Service{
		cache:  cacheRepo,
		store:  storeRepo,
		holds:  holdManager,
		logger: logger,
		tracer: otel.Tracer("balance-service"),
	}
}

// Ready confirms the backing Redis and Postgres dependencies are reachable.
func (s *Service) Ready(ctx context.Context) error {
	if err := s.cache.Ready(ctx); err != nil {
		return err
	}
	if err := s.store.Ready(ctx); err != nil {
		return err
	}
	return nil
}

// Authorize loads account state and atomically reserves funds with a Redis Lua script.
func (s *Service) Authorize(ctx context.Context, request account.AuthorizationRequest) (*account.AuthorizationResult, error) {
	ctx, span := s.tracer.Start(ctx, "balance.authorize", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	snapshot, cacheHit, err := s.loadSnapshot(ctx, request.AccountID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "load snapshot")
		return nil, err
	}

	span.SetAttributes(
		attribute.String("account_id", request.AccountID),
		attribute.String("txn_id", request.TxnID),
		attribute.Bool("cache_hit", cacheHit),
		attribute.Int64("amount_minor", request.MinorUnit),
	)

	if snapshot.Currency != request.Currency {
		return &account.AuthorizationResult{
			Approved:   false,
			ReasonCode: "14",
			HoldID:     request.TxnID,
			Snapshot:   snapshot,
			CacheHit:   cacheHit,
		}, nil
	}

	updated, err := s.holds.Hold(ctx, request.AccountID, request.TxnID, request.MinorUnit)
	if err != nil {
		switch {
		case errors.Is(err, hold.ErrInsufficientFunds):
			return &account.AuthorizationResult{
				Approved:   false,
				ReasonCode: "51",
				HoldID:     request.TxnID,
				Snapshot:   snapshot,
				CacheHit:   cacheHit,
			}, nil
		default:
			span.RecordError(err)
			span.SetStatus(otelcodes.Error, "hold failed")
			return nil, fmt.Errorf("place hold for account %s: %w", request.AccountID, err)
		}
	}

	if err := s.store.UpsertAccount(ctx, updated); err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "persist account")
		return nil, fmt.Errorf("persist updated account %s: %w", request.AccountID, err)
	}

	s.logger.Info(
		"authorized balance hold",
		zap.String("account_id", request.AccountID),
		zap.String("txn_id", request.TxnID),
		zap.Bool("cache_hit", cacheHit),
		zap.Int64("available_minor", updated.AvailableMinor),
	)

	return &account.AuthorizationResult{
		Approved:   true,
		ReasonCode: "00",
		HoldID:     request.TxnID,
		Snapshot:   updated,
		CacheHit:   cacheHit,
	}, nil
}

// Release removes a hold and restores the reserved balance.
func (s *Service) Release(ctx context.Context, accountID, holdID string) (*account.MutationResult, error) {
	ctx, span := s.tracer.Start(ctx, "balance.release", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.SetAttributes(attribute.String("account_id", accountID), attribute.String("hold_id", holdID))

	snapshot, err := s.holds.Release(ctx, accountID, holdID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "release failed")
		return nil, fmt.Errorf("release hold %s: %w", holdID, err)
	}
	if err := s.store.UpsertAccount(ctx, snapshot); err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "persist release")
		return nil, fmt.Errorf("persist release for hold %s: %w", holdID, err)
	}
	return &account.MutationResult{HoldID: holdID, Snapshot: snapshot}, nil
}

// Capture finalizes a hold and moves the reserved amount into the posted balance.
func (s *Service) Capture(ctx context.Context, accountID, holdID string) (*account.MutationResult, error) {
	ctx, span := s.tracer.Start(ctx, "balance.capture", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.SetAttributes(attribute.String("account_id", accountID), attribute.String("hold_id", holdID))

	snapshot, err := s.holds.Capture(ctx, accountID, holdID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "capture failed")
		return nil, fmt.Errorf("capture hold %s: %w", holdID, err)
	}
	if err := s.store.UpsertAccount(ctx, snapshot); err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "persist capture")
		return nil, fmt.Errorf("persist capture for hold %s: %w", holdID, err)
	}
	return &account.MutationResult{HoldID: holdID, Snapshot: snapshot}, nil
}

func (s *Service) loadSnapshot(ctx context.Context, accountID string) (*account.Snapshot, bool, error) {
	snapshot, err := s.cache.Lookup(ctx, accountID)
	if err == nil {
		return snapshot, true, nil
	}
	if !errors.Is(err, cache.ErrAccountNotFound) {
		return nil, false, fmt.Errorf("lookup cached account %s: %w", accountID, err)
	}

	snapshot, err = s.store.GetAccount(ctx, accountID)
	if err != nil {
		return nil, false, fmt.Errorf("load account %s from postgres: %w", accountID, err)
	}
	if err := s.cache.Hydrate(ctx, snapshot); err != nil {
		return nil, false, fmt.Errorf("hydrate cache for account %s: %w", accountID, err)
	}
	s.logger.Info("hydrated account cache from postgres", zap.String("account_id", accountID))
	return snapshot, false, nil
}
