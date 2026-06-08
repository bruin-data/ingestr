//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dynamoDBClient(t *testing.T, ctx context.Context) *dynamodb.Client {
	t.Helper()
	if dynamoDBDest.uri == "" {
		t.Skip("shared dynamodb-local container not available")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(dynamoDBRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			dynamoDBAccessKey, dynamoDBSecretKey, "",
		)),
	)
	require.NoError(t, err)

	// Extract endpoint from the URI
	// URI format: dynamodb://host:port?region=...&access_key_id=...&secret_access_key=...
	endpoint := extractDynamoDBEndpoint(dynamoDBDest.uri)

	return dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func extractDynamoDBEndpoint(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return "http://" + u.Host
}

func scanCount(t *testing.T, ctx context.Context, client *dynamodb.Client, table string) int {
	t.Helper()
	out, err := client.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(table),
		Select:    types.SelectCount,
	})
	require.NoError(t, err)
	return int(out.Count)
}

func getItemStringAttr(item map[string]types.AttributeValue, key string) string {
	av, ok := item[key]
	if !ok {
		return ""
	}
	if s, ok := av.(*types.AttributeValueMemberS); ok {
		return s.Value
	}
	return ""
}

func getItemByID(t *testing.T, ctx context.Context, client *dynamodb.Client, table string, id int) map[string]types.AttributeValue {
	t.Helper()
	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(table),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", id)},
		},
	})
	require.NoError(t, err)
	return out.Item
}

func dynamoDBTableName(suffix string) string {
	return fmt.Sprintf("conformance_%s_%d", suffix, time.Now().UnixNano())
}

func cleanupDynamoDBTable(t *testing.T, ctx context.Context, client *dynamodb.Client, table string) {
	t.Helper()
	_, _ = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(table),
	})
}

func TestDynamoDB_Replace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("replace")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "replace_source",
		DestURI:             dynamoDBDest.uri,
		DestTable:           table,
		IncrementalStrategy: config.StrategyReplace,
		PrimaryKeys:         []string{"id"},
	}

	p := pipeline.New(cfg)
	require.NoError(t, runPipeline(t, ctx, p))

	count := scanCount(t, ctx, client, table)
	assert.Equal(t, replaceFixtureRows, count, "replace should write all rows")

	item := getItemByID(t, ctx, client, table, 1)
	require.NotNil(t, item, "should find item with id=1")
	assert.Equal(t, "alpha", getItemStringAttr(item, "name"))
}

func TestDynamoDB_Append(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("append")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	initialURI := jsonlURI(t, "testdata/conformance_append_initial.jsonl")
	moreURI := jsonlURI(t, "testdata/conformance_append_more.jsonl")

	cfg := &config.IngestConfig{
		SourceURI:           initialURI,
		SourceTable:         "append_source",
		DestURI:             dynamoDBDest.uri,
		DestTable:           table,
		IncrementalStrategy: config.StrategyAppend,
		PrimaryKeys:         []string{"id"},
	}

	p1 := pipeline.New(cfg)
	require.NoError(t, runPipeline(t, ctx, p1))

	count1 := scanCount(t, ctx, client, table)
	assert.Equal(t, appendInitialRows, count1, "initial append should have 5 rows")

	cfg.SourceURI = moreURI
	p2 := pipeline.New(cfg)
	require.NoError(t, runPipeline(t, ctx, p2))

	// append_more has IDs 6-11, initial has IDs 1-5. No overlap, so 11 total.
	count2 := scanCount(t, ctx, client, table)
	assert.Equal(t, appendAfterRows, count2, "after append, should have 11 rows")
}

func TestDynamoDB_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("merge")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	initialURI := jsonlURI(t, "testdata/conformance_merge_initial.jsonl")
	updateURI := jsonlURI(t, "testdata/conformance_merge_update.jsonl")

	cfg := &config.IngestConfig{
		SourceURI:           initialURI,
		SourceTable:         "merge_source",
		DestURI:             dynamoDBDest.uri,
		DestTable:           table,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}

	p1 := pipeline.New(cfg)
	require.NoError(t, runPipeline(t, ctx, p1))

	count1 := scanCount(t, ctx, client, table)
	assert.Equal(t, 5, count1, "initial merge should insert 5 rows")

	cfg.SourceURI = updateURI
	p2 := pipeline.New(cfg)
	require.NoError(t, runPipeline(t, ctx, p2))

	count2 := scanCount(t, ctx, client, table)
	assert.Equal(t, mergeAfterRows, count2, "after merge update, should have 6 rows")

	// Verify updated rows
	item1 := getItemByID(t, ctx, client, table, 1)
	require.NotNil(t, item1)
	assert.Equal(t, "alpha-updated", getItemStringAttr(item1, "name"), "id=1 name should be updated")

	item2 := getItemByID(t, ctx, client, table, 2)
	require.NotNil(t, item2)
	assert.Equal(t, "bravo-updated", getItemStringAttr(item2, "name"), "id=2 name should be updated")

	// Verify new row
	item6 := getItemByID(t, ctx, client, table, 6)
	require.NotNil(t, item6)
	assert.Equal(t, "foxtrot-new", getItemStringAttr(item6, "name"), "id=6 should be inserted")

	// Verify unchanged row
	item3 := getItemByID(t, ctx, client, table, 3)
	require.NotNil(t, item3)
	assert.Equal(t, "charlie", getItemStringAttr(item3, "name"), "id=3 should be unchanged")
}

func TestDynamoDB_ReplaceOverwrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("replace_overwrite")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "replace_source",
		DestURI:             dynamoDBDest.uri,
		DestTable:           table,
		IncrementalStrategy: config.StrategyReplace,
		PrimaryKeys:         []string{"id"},
	}

	// First run
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))
	count1 := scanCount(t, ctx, client, table)
	assert.Equal(t, replaceFixtureRows, count1)

	// Second run with smaller dataset - replace should drop and recreate
	cfg.SourceURI = jsonlURI(t, "testdata/conformance_merge_initial.jsonl")
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	count2 := scanCount(t, ctx, client, table)
	assert.Equal(t, 5, count2, "replace should overwrite with new data only")
}
