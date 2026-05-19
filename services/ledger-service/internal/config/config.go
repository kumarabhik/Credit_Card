package config

import "os"

// Config contains runtime settings for the ledger service.
type Config struct {
	ServiceName      string
	HTTPAddr         string
	GRPCAddr         string
	AWSRegion        string
	DynamoEndpoint   string
	TableName        string
	OTLPGRPCEndpoint string
}

// Load returns environment-backed configuration with local defaults.
func Load() Config {
	return Config{
		ServiceName:      envOrDefault("LEDGER_SERVICE_NAME", "ledger-service"),
		HTTPAddr:         envOrDefault("LEDGER_HTTP_ADDR", ":8083"),
		GRPCAddr:         envOrDefault("LEDGER_GRPC_ADDR", ":9093"),
		AWSRegion:        envOrDefault("AWS_REGION", "us-east-1"),
		DynamoEndpoint:   envOrDefault("LEDGER_DYNAMO_ENDPOINT", "http://127.0.0.1:14566"),
		TableName:        envOrDefault("LEDGER_TABLE_NAME", "cc-ledger-local"),
		OTLPGRPCEndpoint: envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:14319"),
	}
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
