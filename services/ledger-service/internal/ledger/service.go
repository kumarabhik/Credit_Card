package ledger

import (
	"context"
	"fmt"

	ledgerv1 "github.com/kumarabhik/Credit_Card/gen/go/ledger/v1"
	"github.com/kumarabhik/Credit_Card/services/ledger-service/internal/obs"
	"github.com/kumarabhik/Credit_Card/services/ledger-service/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	grpcodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service exposes the ledger gRPC API.
type Service struct {
	ledgerv1.UnimplementedLedgerServiceServer
	repository *store.Repository
	logger     *zap.Logger
	tracer     trace.Tracer
}

// NewService constructs the ledger gRPC service.
func NewService(repository *store.Repository, logger *zap.Logger) *Service {
	return &Service{
		repository: repository,
		logger:     logger,
		tracer:     otel.Tracer("ledger-service"),
	}
}

// Ready confirms the DynamoDB backing table is reachable.
func (s *Service) Ready(ctx context.Context) error {
	return s.repository.Ready(ctx)
}

// Write appends a new immutable ledger entry.
func (s *Service) Write(ctx context.Context, request *ledgerv1.WriteRequest) (*ledgerv1.WriteResponse, error) {
	ctx, span := s.tracer.Start(ctx, "ledger.write", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	if request.GetTxnId() == "" || request.GetAccountId() == "" || request.GetAmount() == nil {
		span.SetStatus(otelcodes.Error, "missing required ledger fields")
		return nil, status.Error(grpcodes.InvalidArgument, "txn_id, account_id, and amount are required")
	}

	span.SetAttributes(
		attribute.String("account_id", request.GetAccountId()),
		attribute.String("txn_id", request.GetTxnId()),
		attribute.String("merchant_id", request.GetMerchantId()),
	)

	response, err := s.repository.Write(ctx, request)
	if err != nil {
		obs.WithTrace(ctx, s.logger).Error(
			"ledger repository write failed",
			zap.String("account_id", request.GetAccountId()),
			zap.String("txn_id", request.GetTxnId()),
			zap.String("idempotency_key", request.GetIdempotencyKey()),
			zap.Error(err),
		)
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "ledger write failed")
		if store.IsConditionalFailure(err) {
			return nil, status.Error(grpcodes.Aborted, "ledger version conflict")
		}
		return nil, status.Error(grpcodes.Internal, "ledger write failed")
	}

	obs.WithTrace(ctx, s.logger).Info(
		"wrote ledger entry",
		zap.String("account_id", request.GetAccountId()),
		zap.String("txn_id", request.GetTxnId()),
		zap.String("ledger_id", response.GetLedgerId()),
		zap.Int64("version", response.GetVersion()),
	)
	return response, nil
}

// Get returns a previously written ledger entry.
func (s *Service) Get(ctx context.Context, request *ledgerv1.GetRequest) (*ledgerv1.GetResponse, error) {
	ctx, span := s.tracer.Start(ctx, "ledger.get", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	if request.GetLedgerId() == "" {
		return nil, status.Error(grpcodes.InvalidArgument, "ledger_id is required")
	}
	record, err := s.repository.Get(ctx, request.GetLedgerId())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "ledger read failed")
		return nil, status.Error(grpcodes.NotFound, fmt.Sprintf("ledger entry %s not found", request.GetLedgerId()))
	}
	return record, nil
}
