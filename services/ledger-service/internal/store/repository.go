package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	commonv1 "github.com/kumarabhik/Credit_Card/gen/go/common/v1"
	ledgerv1 "github.com/kumarabhik/Credit_Card/gen/go/ledger/v1"
	"github.com/oklog/ulid/v2"
)

const (
	stateSortKey = "STATE"
	gsi1Name     = "GSI1"
	gsi2Name     = "GSI2"
)

type dynamoAPI interface {
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	DescribeTable(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
}

type stateItem struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	Version   int64  `dynamodbav:"version"`
	UpdatedAt string `dynamodbav:"updated_at"`
}

type entryItem struct {
	PK             string `dynamodbav:"PK"`
	SK             string `dynamodbav:"SK"`
	LedgerID       string `dynamodbav:"ledger_id"`
	TxnID          string `dynamodbav:"txn_id"`
	AccountID      string `dynamodbav:"account_id"`
	MerchantID     string `dynamodbav:"merchant_id"`
	Type           string `dynamodbav:"type"`
	AmountCurrency string `dynamodbav:"amount_currency"`
	AmountMinor    int64  `dynamodbav:"amount_minor"`
	Version        int64  `dynamodbav:"version"`
	Decision       string `dynamodbav:"decision"`
	RiskScore      int32  `dynamodbav:"risk_score"`
	ReasonCode     string `dynamodbav:"reason_code"`
	IdempotencyKey string `dynamodbav:"idempotency_key"`
	CreatedAt      string `dynamodbav:"created_at"`
	GSI1PK         string `dynamodbav:"gsi1pk"`
	GSI1SK         string `dynamodbav:"gsi1sk"`
	GSI2PK         string `dynamodbav:"gsi2pk"`
	GSI2SK         string `dynamodbav:"gsi2sk"`
}

// Repository handles ledger writes and lookups on the single DynamoDB table.
type Repository struct {
	client dynamoAPI
	table  string
	now    func() time.Time
}

// NewRepositoryFromConfig constructs a Dynamo-backed ledger repository.
func NewRepositoryFromConfig(ctx context.Context, region, endpoint, table string) (*Repository, error) {
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local", "local", "")),
	}
	if endpoint != "" {
		options = append(options, awsconfig.WithBaseEndpoint(endpoint))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &Repository{
		client: dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		}),
		table: table,
		now:   time.Now,
	}, nil
}

// Ready checks whether the ledger table is reachable.
func (r *Repository) Ready(ctx context.Context) error {
	_, err := r.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(r.table),
	})
	if err != nil {
		return fmt.Errorf("describe table %s: %w", r.table, err)
	}
	return nil
}

// Write appends a new immutable ledger entry and advances the account version state item.
func (r *Repository) Write(ctx context.Context, request *ledgerv1.WriteRequest) (*ledgerv1.WriteResponse, error) {
	clock := r.now
	if clock == nil {
		clock = time.Now
	}

	expectedVersion := request.GetVersionExpected()
	if expectedVersion == 0 {
		version, err := r.currentVersion(ctx, request.GetAccountId())
		if err != nil {
			return nil, err
		}
		expectedVersion = version
	}

	now := clock().UTC()
	entryULID := ulid.Make().String()
	ledgerID := encodeLedgerID(request.GetAccountId(), entryULID)
	nextVersion := expectedVersion + 1
	eventID := fmt.Sprintf("evt_%s", strings.ToLower(entryULID))
	accountPartition := accountPartitionKey(request.GetAccountId())

	entry := entryItem{
		PK:             accountPartition,
		SK:             txnSortKey(entryULID),
		LedgerID:       ledgerID,
		TxnID:          request.GetTxnId(),
		AccountID:      request.GetAccountId(),
		MerchantID:     request.GetMerchantId(),
		Type:           request.GetType().String(),
		AmountCurrency: request.GetAmount().GetCurrency(),
		AmountMinor:    request.GetAmount().GetMinorUnits(),
		Version:        nextVersion,
		Decision:       request.GetDecision().String(),
		RiskScore:      request.GetRiskScore(),
		ReasonCode:     request.GetReasonCode(),
		IdempotencyKey: request.GetIdempotencyKey(),
		CreatedAt:      now.Format(time.RFC3339Nano),
		GSI1PK:         fmt.Sprintf("MCH#%s", request.GetMerchantId()),
		GSI1SK:         fmt.Sprintf("TS#%020d#TXN#%s", now.UnixNano(), entryULID),
		GSI2PK:         fmt.Sprintf("IDEMP#%s", request.GetIdempotencyKey()),
		GSI2SK:         fmt.Sprintf("TXN#%s", entryULID),
	}
	entryAV, err := attributevalue.MarshalMap(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal ledger entry for txn %s: %w", request.GetTxnId(), err)
	}

	conditionExpression := "#version = :expected"
	if expectedVersion == 0 {
		conditionExpression = "attribute_not_exists(#version) OR #version = :expected"
	}

	_, err = r.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Update: &types.Update{
					TableName: aws.String(r.table),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: accountPartition},
						"SK": &types.AttributeValueMemberS{Value: stateSortKey},
					},
					UpdateExpression: aws.String(
						"SET #version = :next, updated_at = :updated_at",
					),
					ConditionExpression: aws.String(conditionExpression),
					ExpressionAttributeNames: map[string]string{
						"#version": "version",
					},
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":expected":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", expectedVersion)},
						":next":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", nextVersion)},
						":updated_at": &types.AttributeValueMemberS{Value: now.Format(time.RFC3339Nano)},
					},
				},
			},
			{
				Put: &types.Put{
					TableName:           aws.String(r.table),
					Item:                entryAV,
					ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)"),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("transact ledger write for txn %s: %w", request.GetTxnId(), err)
	}

	return &ledgerv1.WriteResponse{
		LedgerId: ledgerID,
		Version:  nextVersion,
		EventId:  eventID,
	}, nil
}

// Get loads a ledger entry from its encoded ledger ID.
func (r *Repository) Get(ctx context.Context, ledgerID string) (*ledgerv1.GetResponse, error) {
	accountID, entryULID, err := decodeLedgerID(ledgerID)
	if err != nil {
		return nil, err
	}

	output, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(r.table),
		Key:            primaryKey(accountID, entryULID),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("get ledger entry %s: %w", ledgerID, err)
	}
	if len(output.Item) == 0 {
		return nil, fmt.Errorf("ledger entry %s not found", ledgerID)
	}

	record := new(entryItem)
	if err := attributevalue.UnmarshalMap(output.Item, record); err != nil {
		return nil, fmt.Errorf("decode ledger entry %s: %w", ledgerID, err)
	}
	return &ledgerv1.GetResponse{
		LedgerId:   record.LedgerID,
		TxnId:      record.TxnID,
		AccountId:  record.AccountID,
		MerchantId: record.MerchantID,
		Type:       parseEntryType(record.Type),
		Amount: &commonv1.Money{
			Currency:   record.AmountCurrency,
			MinorUnits: record.AmountMinor,
		},
		Version:    record.Version,
		Decision:   parseDecision(record.Decision),
		RiskScore:  record.RiskScore,
		ReasonCode: record.ReasonCode,
	}, nil
}

// LookupByIdempotency queries the GSI2 projection for a specific idempotency key.
func (r *Repository) LookupByIdempotency(ctx context.Context, idempotencyKey string) (*ledgerv1.GetResponse, error) {
	output, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(r.table),
		IndexName:              aws.String(gsi2Name),
		KeyConditionExpression: aws.String("gsi2pk = :gsi2pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gsi2pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("IDEMP#%s", idempotencyKey)},
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("query ledger by idempotency key %s: %w", idempotencyKey, err)
	}
	if len(output.Items) == 0 {
		return nil, fmt.Errorf("idempotency key %s not found", idempotencyKey)
	}

	record := new(entryItem)
	if err := attributevalue.UnmarshalMap(output.Items[0], record); err != nil {
		return nil, fmt.Errorf("decode ledger entry for idempotency key %s: %w", idempotencyKey, err)
	}
	return &ledgerv1.GetResponse{
		LedgerId:   record.LedgerID,
		TxnId:      record.TxnID,
		AccountId:  record.AccountID,
		MerchantId: record.MerchantID,
		Type:       parseEntryType(record.Type),
		Amount: &commonv1.Money{
			Currency:   record.AmountCurrency,
			MinorUnits: record.AmountMinor,
		},
		Version:    record.Version,
		Decision:   parseDecision(record.Decision),
		RiskScore:  record.RiskScore,
		ReasonCode: record.ReasonCode,
	}, nil
}

func (r *Repository) currentVersion(ctx context.Context, accountID string) (int64, error) {
	output, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(r.table),
		Key:            map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: accountPartitionKey(accountID)}, "SK": &types.AttributeValueMemberS{Value: stateSortKey}},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return 0, fmt.Errorf("get current version for account %s: %w", accountID, err)
	}
	if len(output.Item) == 0 {
		return 0, nil
	}

	record := new(stateItem)
	if err := attributevalue.UnmarshalMap(output.Item, record); err != nil {
		return 0, fmt.Errorf("decode current version for account %s: %w", accountID, err)
	}
	return record.Version, nil
}

func primaryKey(accountID, entryULID string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: accountPartitionKey(accountID)},
		"SK": &types.AttributeValueMemberS{Value: txnSortKey(entryULID)},
	}
}

func accountPartitionKey(accountID string) string {
	return fmt.Sprintf("ACCT#%s", accountID)
}

func txnSortKey(entryULID string) string {
	return fmt.Sprintf("TXN#%s", entryULID)
}

func encodeLedgerID(accountID, entryULID string) string {
	return fmt.Sprintf("%s|%s", accountID, entryULID)
}

func decodeLedgerID(ledgerID string) (string, string, error) {
	parts := strings.Split(ledgerID, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid ledger id %q", ledgerID)
	}
	return parts[0], parts[1], nil
}

func parseEntryType(value string) ledgerv1.LedgerEntryType {
	parsed, ok := ledgerv1.LedgerEntryType_value[value]
	if !ok {
		return ledgerv1.LedgerEntryType_LEDGER_ENTRY_TYPE_UNSPECIFIED
	}
	return ledgerv1.LedgerEntryType(parsed)
}

func parseDecision(value string) commonv1.Decision {
	parsed, ok := commonv1.Decision_value[value]
	if !ok {
		return commonv1.Decision_DECISION_UNSPECIFIED
	}
	return commonv1.Decision(parsed)
}

// IsConditionalFailure reports whether Dynamo rejected the optimistic-lock transaction.
func IsConditionalFailure(err error) bool {
	var canceled *types.TransactionCanceledException
	return errors.As(err, &canceled)
}
