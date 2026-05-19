package store

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	ledgerv1 "github.com/kumarabhik/Credit_Card/gen/go/ledger/v1"
	"github.com/stretchr/testify/require"
)

type fakeDynamo struct {
	transactInput *dynamodb.TransactWriteItemsInput
	queryInput    *dynamodb.QueryInput
	getInput      *dynamodb.GetItemInput
}

func (f *fakeDynamo) TransactWriteItems(_ context.Context, params *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	f.transactInput = params
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func (f *fakeDynamo) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.getInput = params

	if params.Key["SK"].(*types.AttributeValueMemberS).Value == stateSortKey {
		item, err := attributevalue.MarshalMap(stateItem{
			PK:      params.Key["PK"].(*types.AttributeValueMemberS).Value,
			SK:      stateSortKey,
			Version: 7,
		})
		if err != nil {
			panic(err)
		}
		return &dynamodb.GetItemOutput{Item: item}, nil
	}

	item, err := attributevalue.MarshalMap(entryItem{
		PK:             params.Key["PK"].(*types.AttributeValueMemberS).Value,
		SK:             params.Key["SK"].(*types.AttributeValueMemberS).Value,
		LedgerID:       "acct_demo|01J0TEST",
		TxnID:          "txn_demo",
		AccountID:      "acct_demo",
		MerchantID:     "mch_demo",
		Type:           ledgerv1.LedgerEntryType_LEDGER_ENTRY_TYPE_AUTHORIZATION.String(),
		AmountCurrency: "USD",
		AmountMinor:    1250,
		Version:        8,
		Decision:       commonv1.Decision_DECISION_APPROVE.String(),
		RiskScore:      127,
		ReasonCode:     "00",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		panic(err)
	}
	return &dynamodb.GetItemOutput{Item: item}, nil
}

func (f *fakeDynamo) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.queryInput = params
	item, err := attributevalue.MarshalMap(entryItem{
		LedgerID:       "acct_demo|01J0TEST",
		TxnID:          "txn_demo",
		AccountID:      "acct_demo",
		MerchantID:     "mch_demo",
		Type:           ledgerv1.LedgerEntryType_LEDGER_ENTRY_TYPE_AUTHORIZATION.String(),
		AmountCurrency: "USD",
		AmountMinor:    1250,
		Version:        8,
		Decision:       commonv1.Decision_DECISION_APPROVE.String(),
		RiskScore:      127,
		ReasonCode:     "00",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		panic(err)
	}
	return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{item}}, nil
}

func (f *fakeDynamo) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return &dynamodb.DescribeTableOutput{}, nil
}

func TestWriteBuildsSingleTableTransaction(t *testing.T) {
	t.Parallel()

	client := new(fakeDynamo)
	repository := &Repository{client: client, table: "cc-ledger-local"}

	response, err := repository.Write(context.Background(), &ledgerv1.WriteRequest{
		TxnId:          "txn_demo",
		AccountId:      "acct_demo",
		Amount:         &commonv1.Money{Currency: "USD", MinorUnits: 1250},
		MerchantId:     "mch_demo",
		Type:           ledgerv1.LedgerEntryType_LEDGER_ENTRY_TYPE_AUTHORIZATION,
		IdempotencyKey: "idem-1",
		Decision:       commonv1.Decision_DECISION_APPROVE,
		RiskScore:      127,
		ReasonCode:     "00",
	})
	require.NoError(t, err)
	require.NotEmpty(t, response.GetLedgerId())
	require.Equal(t, int64(8), response.GetVersion())

	require.Len(t, client.transactInput.TransactItems, 2)
	update := client.transactInput.TransactItems[0].Update
	put := client.transactInput.TransactItems[1].Put

	require.Len(t, update.Key, 2)
	require.Equal(t, "ACCT#acct_demo", update.Key["PK"].(*types.AttributeValueMemberS).Value)
	require.Equal(t, stateSortKey, update.Key["SK"].(*types.AttributeValueMemberS).Value)
	require.Contains(t, aws.ToString(update.ConditionExpression), "#version")

	entry := new(entryItem)
	err = attributevalue.UnmarshalMap(put.Item, entry)
	require.NoError(t, err)
	require.Equal(t, "ACCT#acct_demo", entry.PK)
	require.Contains(t, entry.SK, "TXN#")
	require.Equal(t, "MCH#mch_demo", entry.GSI1PK)
	require.Equal(t, "IDEMP#idem-1", entry.GSI2PK)
}

func TestGetAndLookupByIdempotencyUseEncodedKeyAndIndex(t *testing.T) {
	t.Parallel()

	client := new(fakeDynamo)
	repository := &Repository{client: client, table: "cc-ledger-local"}

	record, err := repository.Get(context.Background(), "acct_demo|01J0TEST")
	require.NoError(t, err)
	require.Equal(t, "acct_demo", record.GetAccountId())
	require.Equal(t, int64(1250), record.GetAmount().GetMinorUnits())

	record, err = repository.LookupByIdempotency(context.Background(), "idem-1")
	require.NoError(t, err)
	require.Equal(t, "txn_demo", record.GetTxnId())
	require.Equal(t, gsi2Name, aws.ToString(client.queryInput.IndexName))
}
