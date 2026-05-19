package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/clients"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/config"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/idempotency"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/obs"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/orchestrator"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	grcodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type authorizeHTTPResponse struct {
	Decision   string `json:"decision"`
	RiskScore  int32  `json:"riskScore"`
	ReasonCode string `json:"reasonCode"`
	AuthCode   string `json:"authCode"`
	TraceID    string `json:"traceId"`
	TxnID      string `json:"txnId"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "auth-service failed: %v\n", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	cfg := config.Load()

	logger, err := obs.NewLogger(cfg.ServiceName)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	_, shutdownTelemetry, err := obs.SetupTelemetry(parent, cfg.ServiceName, cfg.OTLPGRPCEndpoint)
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(ctx)
	}()

	var store idempotency.Store
	store, err = idempotency.NewDynamoStoreFromConfig(parent, cfg.AWSRegion, cfg.DynamoEndpoint, cfg.IdempotencyTable)
	if err != nil {
		logger.Warn("falling back to in-memory idempotency store", zap.Error(err))
		store = idempotency.NewMemoryStore()
	}

	balanceClient := clients.NewBalanceClient(cfg.BalanceBaseURL, 750*time.Millisecond)
	ledgerClient, err := clients.NewLedgerClient(parent, cfg.LedgerGRPCAddr, 1500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("create ledger client: %w", err)
	}
	defer func() { _ = ledgerClient.Close() }()

	service := orchestrator.New(store, logger, balanceClient, ledgerClient)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: newHTTPHandler(service, logger),
	}
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	authv1.RegisterAuthorizationServiceServer(grpcServer, service)

	httpListener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen http on %s: %w", cfg.HTTPAddr, err)
	}
	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc on %s: %w", cfg.GRPCAddr, err)
	}

	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		logger.Info("starting http server", zap.String("addr", cfg.HTTPAddr))
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve http: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		logger.Info("starting grpc server", zap.String("addr", cfg.GRPCAddr))
		if err := grpcServer.Serve(grpcListener); err != nil {
			return fmt.Errorf("serve grpc: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		grpcServer.GracefulStop()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}
		return nil
	})

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func newHTTPHandler(service *orchestrator.Service, logger *zap.Logger) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		if err := service.Ready(ctx); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
	})
	router.Post("/v1/authorize", func(writer http.ResponseWriter, request *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		ctx, span := otel.Tracer("auth-service").Start(ctx, "auth.authorize.http")
		defer span.End()

		if request.Header.Get("Idempotency-Key") == "" {
			span.SetStatus(codes.Error, "missing idempotency header")
			writeError(writer, http.StatusBadRequest, "Idempotency-Key header is required", "")
			return
		}

		body := new(authv1.AuthorizeRequest)
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to read request body")
			writeError(writer, http.StatusBadRequest, "failed to read request body", "")
			return
		}
		if err := protojson.Unmarshal(payload, body); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "invalid json body")
			writeError(writer, http.StatusBadRequest, "request body must be valid JSON", "")
			return
		}
		body.IdempotencyKey = request.Header.Get("Idempotency-Key")

		response, err := service.Authorize(ctx, body)
		if err != nil {
			writeStatusError(writer, logger, ctx, err)
			return
		}

		writeJSON(writer, http.StatusOK, authorizeHTTPResponse{
			Decision:   decisionLabel(response.GetDecision()),
			RiskScore:  response.GetRiskScore(),
			ReasonCode: response.GetReasonCode(),
			AuthCode:   response.GetAuthCode(),
			TraceID:    response.GetTraceId(),
			TxnID:      response.GetTxnId(),
		})
	})

	return router
}

func decisionLabel(decision commonv1.Decision) string {
	switch decision {
	case commonv1.Decision_DECISION_APPROVE:
		return "APPROVE"
	case commonv1.Decision_DECISION_DECLINE:
		return "DECLINE"
	case commonv1.Decision_DECISION_REVIEW:
		return "REVIEW"
	default:
		return "UNSPECIFIED"
	}
}

func writeStatusError(writer http.ResponseWriter, logger *zap.Logger, ctx context.Context, err error) {
	statusError, ok := status.FromError(err)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "internal error", "")
		return
	}

	httpStatus := http.StatusInternalServerError
	switch statusError.Code() {
	case grcodes.InvalidArgument:
		httpStatus = http.StatusBadRequest
	case grcodes.AlreadyExists:
		httpStatus = http.StatusConflict
	}

	obs.WithTrace(ctx, logger).Warn("authorize request failed", zap.String("code", statusError.Code().String()), zap.Error(err))
	writeError(writer, httpStatus, statusError.Message(), trace.SpanContextFromContext(ctx).TraceID().String())
}

func writeError(writer http.ResponseWriter, statusCode int, message, traceID string) {
	payload := map[string]any{
		"error": map[string]string{
			"message":  message,
			"trace_id": traceID,
		},
	}
	writeJSON(writer, statusCode, payload)
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}
