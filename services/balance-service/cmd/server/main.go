package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/account"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/cache"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/config"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/hold"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/obs"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/service"
	"github.com/kumarabhik/Credit_Card/services/balance-service/internal/store"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type authorizeRequest struct {
	AccountID      string          `json:"account_id"`
	TxnID          string          `json:"txn_id"`
	Amount         *commonv1.Money `json:"amount"`
	IdempotencyKey string          `json:"idempotency_key"`
	MerchantID     string          `json:"merchant_id"`
}

type authorizeResponse struct {
	Decision       string            `json:"decision"`
	ReasonCode     string            `json:"reason_code"`
	HoldID         string            `json:"hold_id,omitempty"`
	Account        *account.Snapshot `json:"account,omitempty"`
	CacheHit       bool              `json:"cache_hit"`
	TraceID        string            `json:"trace_id"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

type holdMutationRequest struct {
	AccountID string `json:"account_id"`
	HoldID    string `json:"hold_id"`
}

type holdMutationResponse struct {
	HoldID  string            `json:"hold_id"`
	Account *account.Snapshot `json:"account"`
	TraceID string            `json:"trace_id"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "balance-service failed: %v\n", err)
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

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	db, err := store.OpenDB(cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	balanceService := service.New(
		cache.NewRepository(redisClient),
		store.NewPostgresRepository(db),
		hold.NewManager(redisClient),
		logger,
	)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: newHTTPHandler(balanceService, logger),
	}

	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		logger.Info("starting balance http server", zap.String("addr", cfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve balance http: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown balance http: %w", err)
		}
		return nil
	})

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func newHTTPHandler(balanceService *service.Service, logger *zap.Logger) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		if err := balanceService.Ready(ctx); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
	})
	router.Post("/v1/internal/authorize", func(writer http.ResponseWriter, request *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		ctx, span := otel.Tracer("balance-service").Start(ctx, "balance.authorize.http")
		defer span.End()

		body := new(authorizeRequest)
		if err := json.NewDecoder(request.Body).Decode(body); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "invalid authorize payload")
			writeError(writer, http.StatusBadRequest, "request body must be valid JSON", trace.SpanContextFromContext(ctx).TraceID().String())
			return
		}
		if body.Amount == nil || body.AccountID == "" || body.TxnID == "" {
			writeError(writer, http.StatusBadRequest, "account_id, txn_id, and amount are required", trace.SpanContextFromContext(ctx).TraceID().String())
			return
		}

		result, err := balanceService.Authorize(ctx, account.AuthorizationRequest{
			AccountID: body.AccountID,
			TxnID:     body.TxnID,
			Currency:  body.Amount.GetCurrency(),
			MinorUnit: body.Amount.GetMinorUnits(),
		})
		if err != nil {
			obs.WithTrace(ctx, logger).Error("balance authorize failed", zap.Error(err))
			writeError(writer, http.StatusInternalServerError, "balance authorize failed", trace.SpanContextFromContext(ctx).TraceID().String())
			return
		}

		decision := "DECLINE"
		if result.Approved {
			decision = "APPROVE"
		}
		writeJSON(writer, http.StatusOK, authorizeResponse{
			Decision:       decision,
			ReasonCode:     result.ReasonCode,
			HoldID:         result.HoldID,
			Account:        result.Snapshot,
			CacheHit:       result.CacheHit,
			TraceID:        trace.SpanContextFromContext(ctx).TraceID().String(),
			IdempotencyKey: body.IdempotencyKey,
		})
	})
	router.Post("/v1/internal/release", func(writer http.ResponseWriter, request *http.Request) {
		handleMutation(writer, request, balanceService.Release, "balance.release.http")
	})
	router.Post("/v1/internal/capture", func(writer http.ResponseWriter, request *http.Request) {
		handleMutation(writer, request, balanceService.Capture, "balance.capture.http")
	})
	return router
}

func handleMutation(
	writer http.ResponseWriter,
	request *http.Request,
	mutator func(context.Context, string, string) (*account.MutationResult, error),
	spanName string,
) {
	ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
	ctx, span := otel.Tracer("balance-service").Start(ctx, spanName)
	defer span.End()

	body := new(holdMutationRequest)
	if err := json.NewDecoder(request.Body).Decode(body); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid mutation payload")
		writeError(writer, http.StatusBadRequest, "request body must be valid JSON", trace.SpanContextFromContext(ctx).TraceID().String())
		return
	}
	result, err := mutator(ctx, body.AccountID, body.HoldID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "mutation failed")
		writeError(writer, http.StatusInternalServerError, "balance mutation failed", trace.SpanContextFromContext(ctx).TraceID().String())
		return
	}
	writeJSON(writer, http.StatusOK, holdMutationResponse{
		HoldID:  result.HoldID,
		Account: result.Snapshot,
		TraceID: trace.SpanContextFromContext(ctx).TraceID().String(),
	})
}

func writeError(writer http.ResponseWriter, statusCode int, message, traceID string) {
	writeJSON(writer, statusCode, map[string]any{
		"error": map[string]string{
			"message":  message,
			"trace_id": traceID,
		},
	})
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}
