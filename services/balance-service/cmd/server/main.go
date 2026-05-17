package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	defer func() { _ = logger.Sync() }()

	logger.Info("balance-service skeleton started")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Info("balance-service shutting down", zap.Duration("timeout", 5*time.Second))
	<-shutdownCtx.Done()
	fmt.Fprintln(os.Stdout, "balance-service exited")
}
