package obs

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// NewLogger creates the structured zap logger used by the auth service.
func NewLogger(serviceName string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.DisableCaller = true
	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	return logger.With(zap.String("service", serviceName)), nil
}

// WithTrace enriches the base logger with the active trace ID if one exists.
func WithTrace(ctx context.Context, logger *zap.Logger) *zap.Logger {
	traceID := trace.SpanContextFromContext(ctx).TraceID().String()
	return logger.With(zap.String("trace_id", traceID))
}
