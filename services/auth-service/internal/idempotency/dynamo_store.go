package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	authv1 "github.com/kumarabhik/Credit_Card/gen/go/auth/v1"
)

type dynamoAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	DescribeTable(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
}

type item struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	BodyHash     string `dynamodbav:"body_hash"`
	Status       string `dynamodbav:"status"`
	ResponseJSON string `dynamodbav:"response_json,omitempty"`
	TTL          int64  `dynamodbav:"ttl"`
	UpdatedAt    string `dynamodbav:"updated_at"`
}

// DynamoStore persists idempotency claims in DynamoDB with a conditional PutItem.
type DynamoStore struct {
	client dynamoAPI
	table  string
	now    func() time.Time
}

// NewDynamoStoreFromConfig creates a DynamoDB store using AWS SDK v2 configuration.
func NewDynamoStoreFromConfig(ctx context.Context, region, endpoint, table string) (*DynamoStore, error) {
	options := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local", "local", "")),
	}

	if endpoint != "" {
		options = append(options, config.WithBaseEndpoint(endpoint))
	}

	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &DynamoStore{
		client: dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		}),
		table: table,
		now:   time.Now,
	}, nil
}

// ClaimOrReplay claims a key with PutItem or replays the stored response on duplicates.
func (s *DynamoStore) ClaimOrReplay(ctx context.Context, key string, request *authv1.AuthorizeRequest, ttl time.Duration) (ClaimResult, error) {
	bodyHash, err := RequestHash(request)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("hash request for key %s: %w", key, err)
	}

	record := item{
		PK:        "IDEMP#" + key,
		SK:        "META",
		BodyHash:  bodyHash,
		Status:    "IN_PROGRESS",
		TTL:       s.now().Add(ttl).Unix(),
		UpdatedAt: s.now().UTC().Format(time.RFC3339Nano),
	}
	av, err := attributevalue.MarshalMap(record)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("marshal claim for key %s: %w", key, err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.table),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)"),
	})
	if err == nil {
		return ClaimResult{Status: StatusLeader}, nil
	}

	var conditional *types.ConditionalCheckFailedException
	if !errors.As(err, &conditional) {
		return ClaimResult{}, fmt.Errorf("claim idempotency key %s: %w", key, err)
	}

	return s.waitForExisting(ctx, key, bodyHash)
}

// Complete stores the final authorize response on the claimed row.
func (s *DynamoStore) Complete(ctx context.Context, key string, request *authv1.AuthorizeRequest, response *authv1.AuthorizeResponse, ttl time.Duration) error {
	bodyHash, err := RequestHash(request)
	if err != nil {
		return fmt.Errorf("hash request for key %s: %w", key, err)
	}
	encoded, err := marshalResponse(response)
	if err != nil {
		return fmt.Errorf("marshal response for key %s: %w", key, err)
	}

	_, err = s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table),
		Key:       primaryKey(key),
		ConditionExpression: aws.String(
			"body_hash = :body_hash",
		),
		UpdateExpression: aws.String(
			"SET #status = :status, response_json = :response_json, #ttl = :ttl, updated_at = :updated_at",
		),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
			"#ttl":    "ttl",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":body_hash":     &types.AttributeValueMemberS{Value: bodyHash},
			":status":        &types.AttributeValueMemberS{Value: "COMPLETED"},
			":response_json": &types.AttributeValueMemberS{Value: encoded},
			":ttl":           &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", s.now().Add(ttl).Unix())},
			":updated_at":    &types.AttributeValueMemberS{Value: s.now().UTC().Format(time.RFC3339Nano)},
		},
	})
	if err != nil {
		return fmt.Errorf("complete idempotency key %s: %w", key, err)
	}
	return nil
}

// Abandon removes the in-progress row when the leader fails before completion.
func (s *DynamoStore) Abandon(ctx context.Context, key string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key:       primaryKey(key),
	})
	if err != nil {
		return fmt.Errorf("abandon idempotency key %s: %w", key, err)
	}
	return nil
}

// Ready checks whether the backing table is reachable.
func (s *DynamoStore) Ready(ctx context.Context) error {
	_, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(s.table),
	})
	if err != nil {
		return fmt.Errorf("describe idempotency table %s: %w", s.table, err)
	}
	return nil
}

func (s *DynamoStore) waitForExisting(ctx context.Context, key, bodyHash string) (ClaimResult, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		record, err := s.get(ctx, key)
		if err != nil {
			return ClaimResult{}, err
		}
		if record.BodyHash != bodyHash {
			return ClaimResult{}, ErrConflict
		}
		if record.ResponseJSON != "" && record.Status == "COMPLETED" {
			response, err := unmarshalResponse(record.ResponseJSON)
			if err != nil {
				return ClaimResult{}, fmt.Errorf("decode replay for key %s: %w", key, err)
			}
			return ClaimResult{Status: StatusReplay, Response: response}, nil
		}

		select {
		case <-ctx.Done():
			return ClaimResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *DynamoStore) get(ctx context.Context, key string) (*item, error) {
	output, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.table),
		Key:            primaryKey(key),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("get idempotency key %s: %w", key, err)
	}
	if len(output.Item) == 0 {
		return nil, fmt.Errorf("get idempotency key %s: item missing", key)
	}

	record := new(item)
	if err := attributevalue.UnmarshalMap(output.Item, record); err != nil {
		return nil, fmt.Errorf("decode idempotency key %s: %w", key, err)
	}
	return record, nil
}

func primaryKey(key string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: "IDEMP#" + key},
		"SK": &types.AttributeValueMemberS{Value: "META"},
	}
}
