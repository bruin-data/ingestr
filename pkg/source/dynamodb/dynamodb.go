package dynamodb

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/dynamodbutil"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const batchSize = 10000

type DynamoDBSource struct {
	client *dynamodb.Client
}

func NewDynamoDBSource() *DynamoDBSource {
	return &DynamoDBSource{}
}

func (s *DynamoDBSource) Schemes() []string {
	return []string{"dynamodb"}
}

func (s *DynamoDBSource) HandlesIncrementality() bool {
	return false
}

func (s *DynamoDBSource) Connect(ctx context.Context, uri string) error {
	dbCfg, err := dynamodbutil.ParseURI(uri)
	if err != nil {
		return err
	}

	client, err := dynamodbutil.NewClient(ctx, dbCfg)
	if err != nil {
		return err
	}

	s.client = client
	config.Debug("[DYNAMODB] Connected to region: %s", dbCfg.Region)
	return nil
}

func (s *DynamoDBSource) Close(_ context.Context) error {
	return nil
}

func (s *DynamoDBSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	desc, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe DynamoDB table %q: %w", tableName, err)
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		var hashKey, sortKey string
		for _, key := range desc.Table.KeySchema {
			switch key.KeyType {
			case types.KeyTypeHash:
				hashKey = *key.AttributeName
			case types.KeyTypeRange:
				sortKey = *key.AttributeName
			}
		}
		if hashKey != "" {
			pks = append(pks, hashKey)
		}
		if sortKey != "" {
			pks = append(pks, sortKey)
		}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(_ context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("DynamoDB does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func buildScanArgs(input *dynamodb.ScanInput, opts source.ReadOptions) {
	if opts.IncrementalKey == "" || (opts.IntervalStart == nil && opts.IntervalEnd == nil) {
		return
	}

	input.ExpressionAttributeNames = map[string]string{"#ik": opts.IncrementalKey}
	input.ExpressionAttributeValues = map[string]types.AttributeValue{}

	if opts.IntervalStart != nil && opts.IntervalEnd != nil {
		input.FilterExpression = aws.String("#ik BETWEEN :start AND :end")
		input.ExpressionAttributeValues[":start"] = &types.AttributeValueMemberS{Value: opts.IntervalStart.Format(time.RFC3339)}
		input.ExpressionAttributeValues[":end"] = &types.AttributeValueMemberS{Value: opts.IntervalEnd.Format(time.RFC3339)}
	} else if opts.IntervalStart != nil {
		input.FilterExpression = aws.String("#ik >= :start")
		input.ExpressionAttributeValues[":start"] = &types.AttributeValueMemberS{Value: opts.IntervalStart.Format(time.RFC3339)}
	}
}

func (s *DynamoDBSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[DYNAMODB] Starting read from table: %s", table)

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		input := &dynamodb.ScanInput{
			TableName: aws.String(table),
		}
		buildScanArgs(input, opts)

		paginator := dynamodb.NewScanPaginator(s.client, input)
		batchNum := 0
		totalRows := int64(0)
		var items []map[string]any

		for paginator.HasMorePages() {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			startBatch := time.Now()
			page, err := paginator.NextPage(ctx)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to scan DynamoDB table %q: %w", table, err)}
				return
			}

			for _, item := range page.Items {
				var m map[string]any
				if err := attributevalue.UnmarshalMap(item, &m); err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to unmarshal DynamoDB item: %w", err)}
					return
				}
				items = append(items, m)
			}

			if len(items) >= batchSize || !paginator.HasMorePages() {
				if len(items) == 0 {
					break
				}

				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
					return
				}

				batchNum++
				totalRows += int64(len(items))
				config.Debug("[DYNAMODB] Batch %d: %d items read in %v (total: %d)", batchNum, len(items), time.Since(startBatch), totalRows)

				results <- source.RecordBatchResult{Batch: record}
				items = nil
			}
		}

		config.Debug("[DYNAMODB] Total: %d items in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

var _ source.Source = (*DynamoDBSource)(nil)
