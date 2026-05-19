package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/circuitbreaker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// BalanceAuthorizeRequest is the typed HTTP payload sent to balance-service.
type BalanceAuthorizeRequest struct {
	AccountID      string          `json:"account_id"`
	TxnID          string          `json:"txn_id"`
	Amount         *commonv1.Money `json:"amount"`
	IdempotencyKey string          `json:"idempotency_key"`
	MerchantID     string          `json:"merchant_id"`
}

// BalanceAuthorizeResponse is the typed authorize response from balance-service.
type BalanceAuthorizeResponse struct {
	Decision   string `json:"decision"`
	ReasonCode string `json:"reason_code"`
	HoldID     string `json:"hold_id"`
	CacheHit   bool   `json:"cache_hit"`
}

// BalanceClient wraps the internal balance HTTP API.
type BalanceClient struct {
	baseURL string
	client  *http.Client
	breaker *circuitbreaker.Breaker
	tracer  trace.Tracer
}

// NewBalanceClient constructs a protected balance-service HTTP client.
func NewBalanceClient(baseURL string, timeout time.Duration) *BalanceClient {
	return &BalanceClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
		breaker: circuitbreaker.New("balance-service", 3, 5*time.Second),
		tracer:  otel.Tracer("auth-service"),
	}
}

// Ready checks the downstream balance ready endpoint.
func (c *BalanceClient) Ready(ctx context.Context) error {
	return c.doReady(ctx, c.baseURL+"/readyz")
}

// Authorize calls the internal balance authorize endpoint with timeout, retry, breaker, and trace propagation.
func (c *BalanceClient) Authorize(ctx context.Context, request *BalanceAuthorizeRequest) (*BalanceAuthorizeResponse, error) {
	ctx, span := c.tracer.Start(ctx, "auth.balance.authorize", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(attribute.String("account_id", request.AccountID), attribute.String("txn_id", request.TxnID))

	var response BalanceAuthorizeResponse
	err := retry(ctx, 2, 50*time.Millisecond, func(retryCtx context.Context) error {
		return c.breaker.Execute(retryCtx, func(callCtx context.Context) error {
			payload, err := json.Marshal(request)
			if err != nil {
				return fmt.Errorf("marshal balance authorize request: %w", err)
			}
			httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.baseURL+"/v1/internal/authorize", bytes.NewReader(payload))
			if err != nil {
				return fmt.Errorf("create balance authorize request: %w", err)
			}
			httpRequest.Header.Set("Content-Type", "application/json")
			otel.GetTextMapPropagator().Inject(callCtx, propagation.HeaderCarrier(httpRequest.Header))

			httpResponse, err := c.client.Do(httpRequest)
			if err != nil {
				return fmt.Errorf("call balance authorize endpoint: %w", err)
			}
			defer httpResponse.Body.Close()

			if httpResponse.StatusCode >= 500 {
				return fmt.Errorf("balance authorize returned %d", httpResponse.StatusCode)
			}
			if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
				return fmt.Errorf("decode balance authorize response: %w", err)
			}
			return nil
		})
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return &response, nil
}

func (c *BalanceClient) doReady(ctx context.Context, url string) error {
	return c.breaker.Execute(ctx, func(callCtx context.Context) error {
		request, err := http.NewRequestWithContext(callCtx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create ready request: %w", err)
		}
		otel.GetTextMapPropagator().Inject(callCtx, propagation.HeaderCarrier(request.Header))
		response, err := c.client.Do(request)
		if err != nil {
			return fmt.Errorf("call ready endpoint: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("ready endpoint returned %d", response.StatusCode)
		}
		return nil
	})
}
