package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	ledgerv1 "github.com/kumarabhik/Credit_Card/gen/go/ledger/v1"
	"github.com/kumarabhik/Credit_Card/services/ledger-service/internal/config"
	"github.com/kumarabhik/Credit_Card/services/ledger-service/internal/ledger"
	"github.com/kumarabhik/Credit_Card/services/ledger-service/internal/obs"
	"github.com/kumarabhik/Credit_Card/services/ledger-service/internal/store"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "ledger-service failed: %v\n", err)
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

	repository, err := store.NewRepositoryFromConfig(parent, cfg.AWSRegion, cfg.DynamoEndpoint, cfg.TableName)
	if err != nil {
		return err
	}
	service := ledger.NewService(repository, logger)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: newHTTPHandler(service),
	}
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	ledgerv1.RegisterLedgerServiceServer(grpcServer, service)

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
		logger.Info("starting ledger http server", zap.String("addr", cfg.HTTPAddr))
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve ledger http: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		logger.Info("starting ledger grpc server", zap.String("addr", cfg.GRPCAddr))
		if err := grpcServer.Serve(grpcListener); err != nil {
			return fmt.Errorf("serve ledger grpc: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		grpcServer.GracefulStop()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown ledger http: %w", err)
		}
		return nil
	})

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func newHTTPHandler(service *ledger.Service) http.Handler {
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
	return router
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}
