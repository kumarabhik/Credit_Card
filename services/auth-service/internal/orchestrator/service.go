package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/idempotency"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	grpcodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service implements the authorize hot path skeleton.
type Service struct {
	authv1.UnimplementedAuthorizationServiceServer
	store     idempotency.Store
	logger    *zap.Logger
	tracer    trace.Tracer
	responder func(context.Context) *authv1.AuthorizeResponse
}

// New returns the auth orchestrator service.
func New(store idempotency.Store, logger *zap.Logger) *Service {
	return &Service{
		store:  store,
		logger: logger,
		tracer: otel.Tracer("auth-service"),
		responder: func(ctx context.Context) *authv1.AuthorizeResponse {
			traceID := trace.SpanContextFromContext(ctx).TraceID().String()
			authCode := strings.ToUpper(strings.ReplaceAll(traceID, "-", ""))
			if len(authCode) > 6 {
				authCode = authCode[:6]
			}

			return &authv1.AuthorizeResponse{
				Decision:   commonv1.Decision_DECISION_APPROVE,
				RiskScore:  127,
				ReasonCode: "00",
				AuthCode:   authCode,
				TraceId:    traceID,
				TxnId:      fmt.Sprintf("txn_%s", uuid.NewString()[:12]),
			}
		},
	}
}

// SetResponder overrides the response factory used by authorize.
func (s *Service) SetResponder(responder func(context.Context) *authv1.AuthorizeResponse) {
	s.responder = responder
}

// Ready reports whether downstream dependencies needed by the auth service are reachable.
func (s *Service) Ready(ctx context.Context) error {
	return s.store.Ready(ctx)
}

// Authorize handles the MVP authorize request flow with idempotent response replay.
func (s *Service) Authorize(ctx context.Context, request *authv1.AuthorizeRequest) (*authv1.AuthorizeResponse, error) {
	ctx, span := s.tracer.Start(ctx, "auth.orchestrate", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	if request.GetIdempotencyKey() == "" {
		span.SetStatus(otelcodes.Error, "missing idempotency key")
		return nil, status.Error(grpcodes.InvalidArgument, "idempotency_key is required")
	}
	if request.GetCardToken() == "" {
		span.SetStatus(otelcodes.Error, "missing card token")
		return nil, status.Error(grpcodes.InvalidArgument, "card_token is required")
	}

	span.SetAttributes(
		attribute.String("merchant_id", request.GetMerchantId()),
		attribute.String("idempotency_key", request.GetIdempotencyKey()),
		attribute.String("channel", request.GetChannel()),
	)

	claim, err := s.store.ClaimOrReplay(ctx, request.GetIdempotencyKey(), request, 24*time.Hour)
	if err != nil {
		switch {
		case err == idempotency.ErrConflict:
			span.SetStatus(otelcodes.Error, "idempotency conflict")
			return nil, status.Error(grpcodes.AlreadyExists, "idempotency key reused with a different request body")
		default:
			span.RecordError(err)
			span.SetStatus(otelcodes.Error, "idempotency claim failed")
			return nil, status.Error(grpcodes.Internal, "idempotency claim failed")
		}
	}

	if claim.Status == idempotency.StatusReplay {
		obs.WithTrace(ctx, s.logger).Info(
			"replayed authorize response",
			zap.String("merchant_id", request.GetMerchantId()),
			zap.String("txn_id", claim.Response.GetTxnId()),
			zap.Bool("replayed", true),
		)
		return claim.Response, nil
	}

	response := s.responder(ctx)
	if err := s.store.Complete(ctx, request.GetIdempotencyKey(), request, response, 24*time.Hour); err != nil {
		_ = s.store.Abandon(ctx, request.GetIdempotencyKey())
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "store completion failed")
		return nil, status.Error(grpcodes.Internal, "failed to persist authorize response")
	}

	obs.WithTrace(ctx, s.logger).Info(
		"authorized request",
		zap.String("merchant_id", request.GetMerchantId()),
		zap.String("txn_id", response.GetTxnId()),
		zap.Bool("replayed", false),
	)

	return response, nil
}

// Capture is not implemented in the MVP skeleton yet.
func (s *Service) Capture(context.Context, *authv1.CaptureRequest) (*authv1.CaptureResponse, error) {
	return nil, status.Error(grpcodes.Unimplemented, "capture is not implemented yet")
}

// Reverse is not implemented in the MVP skeleton yet.
func (s *Service) Reverse(context.Context, *authv1.ReverseRequest) (*authv1.ReverseResponse, error) {
	return nil, status.Error(grpcodes.Unimplemented, "reverse is not implemented yet")
}

// Refund is not implemented in the MVP skeleton yet.
func (s *Service) Refund(context.Context, *authv1.RefundRequest) (*authv1.RefundResponse, error) {
	return nil, status.Error(grpcodes.Unimplemented, "refund is not implemented yet")
}
