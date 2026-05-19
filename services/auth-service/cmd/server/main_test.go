package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/idempotency"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/obs"
	"github.com/kumarabhik/Credit_Card/services/auth-service/internal/orchestrator"
	"github.com/stretchr/testify/require"
)

func TestAuthorizeHTTPPropagatesTraceparent(t *testing.T) {
	t.Parallel()

	logger, err := obs.NewLogger("auth-service-test")
	require.NoError(t, err)

	_, shutdown, err := obs.SetupTelemetry(context.Background(), "auth-service-test", "")
	require.NoError(t, err)
	defer func() { _ = shutdown(context.Background()) }()

	service := orchestrator.New(idempotency.NewMemoryStore(), logger, nil, nil)
	handler := newHTTPHandler(service, logger)

	payload := map[string]any{
		"card_token":  "tok_demo_card",
		"amount":      map[string]any{"currency": "USD", "minor_units": 2599},
		"merchant_id": "mch_demo_grocery",
		"channel":     "POS",
		"device_id":   "device-terminal-01",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/authorize", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "trace-demo")
	request.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	result := new(authorizeHTTPResponse)
	err = json.Unmarshal(response.Body.Bytes(), result)
	require.NoError(t, err)
	require.Equal(t, "0123456789abcdef0123456789abcdef", result.TraceID)
	require.Equal(t, "APPROVE", result.Decision)
}

func TestAuthorizeDeduplicatesConcurrentRequests(t *testing.T) {
	t.Parallel()

	logger, err := obs.NewLogger("auth-service-test")
	require.NoError(t, err)

	service := orchestrator.New(idempotency.NewMemoryStore(), logger, nil, nil)
	request := &authv1.AuthorizeRequest{
		IdempotencyKey: "fanout-key",
		CardToken:      "tok_demo_card",
		MerchantId:     "mch_demo_grocery",
		Channel:        "POS",
		DeviceId:       "device-terminal-01",
	}

	var count atomic.Int64
	service.SetResponder(func(ctx context.Context) *authv1.AuthorizeResponse {
		count.Add(1)
		return &authv1.AuthorizeResponse{
			TraceId: "trace-value",
			TxnId:   "txn-shared",
		}
	})

	const workers = 16
	results := make(chan *authv1.AuthorizeResponse, workers)
	errorsCh := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer group.Done()
			response, err := service.Authorize(context.Background(), request)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- response
		}()
	}

	group.Wait()
	close(results)
	close(errorsCh)

	for err := range errorsCh {
		require.NoError(t, err)
	}

	for result := range results {
		require.Equal(t, "txn-shared", result.GetTxnId())
	}
	require.Equal(t, int64(1), count.Load())
}
