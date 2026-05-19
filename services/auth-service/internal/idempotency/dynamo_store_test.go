package idempotency

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	"github.com/stretchr/testify/require"
)

type fakeDynamo struct {
	putItemInput    *dynamodb.PutItemInput
	getItemOutput   *dynamodb.GetItemOutput
	putItemError    error
	updateItemInput *dynamodb.UpdateItemInput
}

func (f *fakeDynamo) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.putItemInput = params
	if f.putItemError != nil {
		return nil, f.putItemError
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeDynamo) GetItem(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return f.getItemOutput, nil
}

func (f *fakeDynamo) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updateItemInput = params
	return &dynamodb.UpdateItemOutput{}, nil
}

func (f *fakeDynamo) DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	return &dynamodb.DeleteItemOutput{}, nil
}

func (f *fakeDynamo) DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return &dynamodb.DescribeTableOutput{}, nil
}

func TestDynamoStoreClaimUsesConditionalPut(t *testing.T) {
	t.Parallel()

	client := &fakeDynamo{}
	store := &DynamoStore{client: client, table: "cc-ledger-local", now: time.Now}

	_, err := store.ClaimOrReplay(context.Background(), "demo-key", sampleRequest(), 24*time.Hour)
	require.NoError(t, err)
	require.NotNil(t, client.putItemInput)
	require.Equal(t, "attribute_not_exists(PK) AND attribute_not_exists(SK)", *client.putItemInput.ConditionExpression)
}

func TestDynamoStoreConflictOnBodyMismatch(t *testing.T) {
	t.Parallel()

	client := &fakeDynamo{
		putItemError: &types.ConditionalCheckFailedException{},
		getItemOutput: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"PK":        &types.AttributeValueMemberS{Value: "IDEMP#demo-key"},
				"SK":        &types.AttributeValueMemberS{Value: "META"},
				"body_hash": &types.AttributeValueMemberS{Value: "other-hash"},
				"status":    &types.AttributeValueMemberS{Value: "COMPLETED"},
				"ttl":       &types.AttributeValueMemberN{Value: "1"},
			},
		},
	}
	store := &DynamoStore{client: client, table: "cc-ledger-local", now: time.Now}

	_, err := store.ClaimOrReplay(context.Background(), "demo-key", sampleRequest(), 24*time.Hour)
	require.ErrorIs(t, err, ErrConflict)
}

func TestDynamoStoreReplaysCompletedResponse(t *testing.T) {
	t.Parallel()

	request := sampleRequest()
	response := sampleResponse()
	encoded, err := marshalResponse(response)
	require.NoError(t, err)
	hash, err := RequestHash(request)
	require.NoError(t, err)

	client := &fakeDynamo{
		putItemError: &types.ConditionalCheckFailedException{},
		getItemOutput: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"PK":            &types.AttributeValueMemberS{Value: "IDEMP#demo-key"},
				"SK":            &types.AttributeValueMemberS{Value: "META"},
				"body_hash":     &types.AttributeValueMemberS{Value: hash},
				"status":        &types.AttributeValueMemberS{Value: "COMPLETED"},
				"response_json": &types.AttributeValueMemberS{Value: encoded},
				"ttl":           &types.AttributeValueMemberN{Value: "1"},
			},
		},
	}
	store := &DynamoStore{client: client, table: "cc-ledger-local", now: time.Now}

	result, err := store.ClaimOrReplay(context.Background(), "demo-key", request, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, StatusReplay, result.Status)
	require.Equal(t, response.GetTxnId(), result.Response.GetTxnId())
}

func TestDynamoStoreCompleteWritesCachedResponse(t *testing.T) {
	t.Parallel()

	client := &fakeDynamo{}
	store := &DynamoStore{client: client, table: "cc-ledger-local", now: time.Now}

	err := store.Complete(context.Background(), "demo-key", sampleRequest(), sampleResponse(), 24*time.Hour)
	require.NoError(t, err)
	require.NotNil(t, client.updateItemInput)
	require.Contains(t, *client.updateItemInput.UpdateExpression, "response_json")
	require.Contains(t, *client.updateItemInput.UpdateExpression, "#ttl")
	require.Equal(t, "ttl", client.updateItemInput.ExpressionAttributeNames["#ttl"])
}

func sampleRequest() *authv1.AuthorizeRequest {
	return &authv1.AuthorizeRequest{
		IdempotencyKey: "demo-key",
		CardToken:      "tok_demo_card",
		Amount:         &commonv1.Money{Currency: "USD", MinorUnits: 2500},
		MerchantId:     "mch_demo_grocery",
		Channel:        "POS",
		DeviceId:       "device-1",
	}
}

func sampleResponse() *authv1.AuthorizeResponse {
	return &authv1.AuthorizeResponse{
		Decision:   commonv1.Decision_DECISION_APPROVE,
		RiskScore:  127,
		ReasonCode: "00",
		AuthCode:   "ABC123",
		TraceId:    "trace-id",
		TxnId:      "txn-demo",
	}
}
