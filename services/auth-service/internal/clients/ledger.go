package clients

import (
	"context"
	"fmt"
	"time"

	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	ledgerv1 "github.com/kumarabhik/Credit_Card/gen/go/ledger/v1"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/circuitbreaker"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// LedgerClient wraps the ledger gRPC API.
type LedgerClient struct {
	conn    *grpc.ClientConn
	client  ledgerv1.LedgerServiceClient
	breaker *circuitbreaker.Breaker
	tracer  trace.Tracer
}

// NewLedgerClient dials the ledger gRPC endpoint.
func NewLedgerClient(ctx context.Context, address string, timeout time.Duration) (*LedgerClient, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial ledger grpc %s: %w", address, err)
	}

	return &LedgerClient{
		conn:    conn,
		client:  ledgerv1.NewLedgerServiceClient(conn),
		breaker: circuitbreaker.New("ledger-service", 3, 5*time.Second),
		tracer:  otel.Tracer("auth-service"),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *LedgerClient) Close() error {
	return c.conn.Close()
}

// Ready verifies the ledger gRPC connection can establish readiness.
func (c *LedgerClient) Ready(ctx context.Context) error {
	return c.breaker.Execute(ctx, func(callCtx context.Context) error {
		state := c.conn.GetState()
		if state.String() == "READY" || state.String() == "IDLE" || state.String() == "CONNECTING" {
			return nil
		}
		return fmt.Errorf("ledger connection state is %s", state)
	})
}

// Write appends a ledger entry with timeout, retry, breaker, and trace propagation.
func (c *LedgerClient) Write(ctx context.Context, txnID, accountID, merchantID, idempotencyKey string, amount *commonv1.Money) (*ledgerv1.WriteResponse, error) {
	ctx, span := c.tracer.Start(ctx, "auth.ledger.write", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(attribute.String("account_id", accountID), attribute.String("txn_id", txnID))

	var response *ledgerv1.WriteResponse
	err := retry(ctx, 2, 50*time.Millisecond, func(retryCtx context.Context) error {
		return c.breaker.Execute(retryCtx, func(callCtx context.Context) error {
			grpcCtx, cancel := context.WithTimeout(callCtx, 750*time.Millisecond)
			defer cancel()

			var err error
			response, err = c.client.Write(grpcCtx, &ledgerv1.WriteRequest{
				TxnId:           txnID,
				AccountId:       accountID,
				Amount:          amount,
				MerchantId:      merchantID,
				Type:            ledgerv1.LedgerEntryType_LEDGER_ENTRY_TYPE_AUTHORIZATION,
				IdempotencyKey:  idempotencyKey,
				VersionExpected: 0,
				Decision:        commonv1.Decision_DECISION_APPROVE,
				RiskScore:       127,
				ReasonCode:      "00",
			})
			if err != nil {
				return fmt.Errorf("call ledger write: %w", err)
			}
			return nil
		})
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return response, nil
}
