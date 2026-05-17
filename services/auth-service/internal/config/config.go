package config

import "os"

// Config contains runtime settings for the auth service.
type Config struct {
	ServiceName      string
	HTTPAddr         string
	GRPCAddr         string
	AWSRegion        string
	DynamoEndpoint   string
	IdempotencyTable string
	OTLPGRPCEndpoint string
}

// Load returns the environment-backed configuration with safe local defaults.
func Load() Config {
	return Config{
		ServiceName:      envOrDefault("AUTH_SERVICE_NAME", "auth-service"),
		HTTPAddr:         envOrDefault("AUTH_HTTP_ADDR", ":8081"),
		GRPCAddr:         envOrDefault("AUTH_GRPC_ADDR", ":9091"),
		AWSRegion:        envOrDefault("AWS_REGION", "us-east-1"),
		DynamoEndpoint:   os.Getenv("AUTH_DYNAMO_ENDPOINT"),
		IdempotencyTable: envOrDefault("AUTH_IDEMPOTENCY_TABLE", "cc-ledger-local"),
		OTLPGRPCEndpoint: envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:14319"),
	}
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
