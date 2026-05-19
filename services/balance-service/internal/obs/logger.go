package obs

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// NewLogger creates the structured zap logger used by the balance service.
func NewLogger(serviceName string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.DisableCaller = true
	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	return logger.With(zap.String("service", serviceName)), nil
}

// WithTrace enriches the logger with the active trace ID.
func WithTrace(ctx context.Context, logger *zap.Logger) *zap.Logger {
	return logger.With(zap.String("trace_id", trace.SpanContextFromContext(ctx).TraceID().String()))
}
