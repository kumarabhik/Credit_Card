package config

import "os"

// Config contains runtime settings for the balance service.
type Config struct {
	ServiceName      string
	HTTPAddr         string
	RedisAddr        string
	PostgresDSN      string
	OTLPGRPCEndpoint string
}

// Load returns environment-backed configuration with local development defaults.
func Load() Config {
	return Config{
		ServiceName:      envOrDefault("BALANCE_SERVICE_NAME", "balance-service"),
		HTTPAddr:         envOrDefault("BALANCE_HTTP_ADDR", ":8082"),
		RedisAddr:        envOrDefault("BALANCE_REDIS_ADDR", "127.0.0.1:16379"),
		PostgresDSN:      envOrDefault("BALANCE_POSTGRES_DSN", "postgres://postgres:postgres@127.0.0.1:15432/cc?sslmode=disable"),
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
