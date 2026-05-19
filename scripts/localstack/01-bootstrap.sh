#!/bin/sh
set -eu

region="${AWS_DEFAULT_REGION:-us-east-1}"

create_topic() {
  awslocal sns create-topic --name "$1" --query TopicArn --output text >/dev/null
}

create_queue() {
  name="$1"
  attributes="$2"
  awslocal sqs create-queue --queue-name "$name" --attributes "$attributes" --query QueueUrl --output text >/dev/null
}

create_standard_queue() {
  awslocal sqs create-queue --queue-name "$1" --query QueueUrl --output text >/dev/null
}

ensure_dynamo_table() {
  table_name="$1"

  if awslocal dynamodb describe-table --table-name "$table_name" >/dev/null 2>&1; then
    return 0
  fi

  attempts=0
  until awslocal dynamodb create-table \
    --table-name "$table_name" \
    --attribute-definitions \
      AttributeName=PK,AttributeType=S \
      AttributeName=SK,AttributeType=S \
      AttributeName=gsi1pk,AttributeType=S \
      AttributeName=gsi1sk,AttributeType=S \
      AttributeName=gsi2pk,AttributeType=S \
      AttributeName=gsi2sk,AttributeType=S \
    --key-schema \
      AttributeName=PK,KeyType=HASH \
      AttributeName=SK,KeyType=RANGE \
    --billing-mode PAY_PER_REQUEST \
    --global-secondary-indexes \
      "IndexName=GSI1,KeySchema=[{AttributeName=gsi1pk,KeyType=HASH},{AttributeName=gsi1sk,KeyType=RANGE}],Projection={ProjectionType=ALL}" \
      "IndexName=GSI2,KeySchema=[{AttributeName=gsi2pk,KeyType=HASH},{AttributeName=gsi2sk,KeyType=RANGE}],Projection={ProjectionType=ALL}" \
    >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 20 ]; then
      echo "failed to create DynamoDB table $table_name after $attempts attempts" >&2
      return 1
    fi
    sleep 1
    if awslocal dynamodb describe-table --table-name "$table_name" >/dev/null 2>&1; then
      break
    fi
  done

  attempts=0
  until awslocal dynamodb describe-table --table-name "$table_name" --query 'Table.TableStatus' --output text 2>/dev/null | grep -q '^ACTIVE$'; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 20 ]; then
      echo "DynamoDB table $table_name did not become ACTIVE" >&2
      return 1
    fi
    sleep 1
  done
}

create_topic "txn-authorized"
create_topic "txn-declined"
create_topic "txn-captured"
create_topic "txn-reversed"

create_queue "settlement.fifo" "FifoQueue=true,ContentBasedDeduplication=true"
create_standard_queue "notification"
create_standard_queue "analytics"

ensure_dynamo_table "cc-ledger-local"

awslocal kms create-key --description "Local smoke tokenization key" --query KeyMetadata.KeyId --output text >/dev/null

awslocal secretsmanager create-secret \
  --name "cc/local/jwt-signing-key" \
  --secret-string '{"kid":"local-dev","private_key":"placeholder","public_key":"placeholder"}' \
  >/dev/null 2>&1 || true

awslocal secretsmanager create-secret \
  --name "cc/local/webhook-secret" \
  --secret-string '{"merchant_id":"mch_demo_grocery","secret":"local-webhook-secret"}' \
  >/dev/null 2>&1 || true

printf 'LocalStack bootstrap complete in %s\n' "$region"
