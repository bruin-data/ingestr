package dynamodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/dynamodbutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const maxBatchWriteItems = 25

type DynamoDBDestination struct {
	client    *dynamodb.Client
	tablePKs  []string
	tableName string
}

func NewDynamoDBDestination() *DynamoDBDestination {
	return &DynamoDBDestination{}
}

func (d *DynamoDBDestination) Schemes() []string {
	return []string{"dynamodb"}
}

func (d *DynamoDBDestination) Connect(ctx context.Context, uri string) error {
	dbCfg, err := dynamodbutil.ParseURI(uri)
	if err != nil {
		return err
	}

	client, err := dynamodbutil.NewClient(ctx, dbCfg)
	if err != nil {
		return err
	}

	d.client = client
	config.Debug("[DYNAMODB DEST] Connected to region: %s", dbCfg.Region)
	return nil
}

func (d *DynamoDBDestination) Close(_ context.Context) error {
	return nil
}

func (d *DynamoDBDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	tableName := opts.Table

	pks := opts.PrimaryKeys
	if len(pks) > 0 {
		d.tablePKs = pks
		d.tableName = tableName
	} else {
		// For staging tables (merge strategy passes PrimaryKeys=nil),
		// fall back to previously stored PKs or schema PKs.
		pks = d.tablePKs
		if len(pks) == 0 && opts.Schema != nil {
			pks = opts.Schema.PrimaryKeys
		}
	}

	if opts.DropFirst {
		if err := d.deleteTableIfExists(ctx, tableName); err != nil {
			return err
		}
	}

	exists, err := d.tableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	return d.createTable(ctx, tableName, opts.Schema, pks)
}

func (d *DynamoDBDestination) tableExists(ctx context.Context, table string) (bool, error) {
	_, err := d.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(table),
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to describe table %q: %w", table, err)
	}
	return true, nil
}

func (d *DynamoDBDestination) deleteTableIfExists(ctx context.Context, table string) error {
	_, err := d.client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(table),
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("failed to delete table %q: %w", table, err)
	}

	waiter := dynamodb.NewTableNotExistsWaiter(d.client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(table),
	}, 2*time.Minute); err != nil {
		return fmt.Errorf("timed out waiting for table %q deletion: %w", table, err)
	}

	config.Debug("[DYNAMODB DEST] Deleted table: %s", table)
	return nil
}

func (d *DynamoDBDestination) createTable(ctx context.Context, table string, sch *schema.TableSchema, pks []string) error {
	if len(pks) == 0 {
		return fmt.Errorf("DynamoDB requires at least one primary key")
	}
	if len(pks) > 2 {
		return fmt.Errorf("DynamoDB supports at most 2 key attributes (hash + range), got %d", len(pks))
	}

	attrDefs := make([]types.AttributeDefinition, 0, len(pks))
	keySchema := make([]types.KeySchemaElement, 0, len(pks))

	for i, pk := range pks {
		attrType := types.ScalarAttributeTypeS
		if sch != nil {
			for _, col := range sch.Columns {
				if col.Name == pk {
					attrType = schemaTypeToDynamoDBScalar(col.DataType)
					break
				}
			}
		}

		attrDefs = append(attrDefs, types.AttributeDefinition{
			AttributeName: aws.String(pk),
			AttributeType: attrType,
		})

		keyType := types.KeyTypeHash
		if i == 1 {
			keyType = types.KeyTypeRange
		}
		keySchema = append(keySchema, types.KeySchemaElement{
			AttributeName: aws.String(pk),
			KeyType:       keyType,
		})
	}

	_, err := d.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:            aws.String(table),
		AttributeDefinitions: attrDefs,
		KeySchema:            keySchema,
		BillingMode:          types.BillingModePayPerRequest,
	})
	if err != nil {
		return fmt.Errorf("failed to create table %q: %w", table, err)
	}

	waiter := dynamodb.NewTableExistsWaiter(d.client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(table),
	}, 2*time.Minute); err != nil {
		return fmt.Errorf("timed out waiting for table %q creation: %w", table, err)
	}

	config.Debug("[DYNAMODB DEST] Created table: %s", table)
	return nil
}

func schemaTypeToDynamoDBScalar(dt schema.DataType) types.ScalarAttributeType {
	switch dt {
	case schema.TypeInt16, schema.TypeInt32, schema.TypeInt64,
		schema.TypeFloat32, schema.TypeFloat64, schema.TypeDecimal:
		return types.ScalarAttributeTypeN
	default:
		return types.ScalarAttributeTypeS
	}
}

func (d *DynamoDBDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTotal := time.Now()
	config.Debug("[DYNAMODB DEST] Waiting for records...")

	tableName := opts.Table
	if tableName == "" {
		tableName = d.tableName
	}

	batchNum := 0
	var totalRows int64

	for result := range records {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batchNum++
		if result.Err != nil {
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}
		if record.NumRows() == 0 {
			record.Release()
			continue
		}

		startBatch := time.Now()
		rows, err := d.writeBatch(ctx, tableName, record)
		record.Release()
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		config.Debug("[DYNAMODB DEST] Batch %d: %d items in %v", batchNum, rows, time.Since(startBatch))
	}

	config.Debug("[DYNAMODB DEST] Total: %d items written in %v", totalRows, time.Since(startTotal))
	return nil
}

func (d *DynamoDBDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	config.Debug("[DYNAMODB DEST] Starting parallel write with %d workers", parallelism)
	startTotal := time.Now()

	tableName := opts.Table
	if tableName == "" {
		tableName = d.tableName
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type writeResult struct {
		batchNum int
		rows     int64
		duration time.Duration
		err      error
	}

	results := make(chan writeResult, parallelism*2)
	var wg sync.WaitGroup
	batchNum := int64(0)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range records {
				if ctx.Err() != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					return
				}

				myBatch := int(atomic.AddInt64(&batchNum, 1))
				if result.Err != nil {
					results <- writeResult{batchNum: myBatch, err: result.Err}
					cancel()
					return
				}

				record := result.Batch
				if record == nil {
					continue
				}
				if record.NumRows() == 0 {
					record.Release()
					continue
				}

				startBatch := time.Now()
				rows, err := d.writeBatch(ctx, tableName, record)
				record.Release()

				results <- writeResult{
					batchNum: myBatch,
					rows:     rows,
					duration: time.Since(startBatch),
					err:      err,
				}

				if err != nil {
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var totalRows int64
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if res.err == nil {
			totalRows += res.rows
			config.Debug("[DYNAMODB DEST] Batch %d: %d items in %v", res.batchNum, res.rows, res.duration)
		}
	}

	if firstErr != nil {
		return fmt.Errorf("parallel write failed: %w", firstErr)
	}

	config.Debug("[DYNAMODB DEST] Total: %d items written in %v", totalRows, time.Since(startTotal))
	return nil
}

func (d *DynamoDBDestination) writeBatch(ctx context.Context, table string, record arrow.RecordBatch) (int64, error) {
	rows := int(record.NumRows())
	cols := int(record.NumCols())
	if rows == 0 {
		return 0, nil
	}

	columns := make([]string, cols)
	for i := 0; i < cols; i++ {
		columns[i] = record.ColumnName(i)
	}

	var totalWritten int64
	chunk := make([]types.WriteRequest, 0, maxBatchWriteItems)

	for row := 0; row < rows; row++ {
		select {
		case <-ctx.Done():
			return totalWritten, ctx.Err()
		default:
		}

		item := make(map[string]types.AttributeValue, cols)
		for col := 0; col < cols; col++ {
			av := arrowToDynamoDB(record.Column(col), row)
			if av != nil {
				item[columns[col]] = av
			}
		}

		chunk = append(chunk, types.WriteRequest{
			PutRequest: &types.PutRequest{Item: item},
		})

		if len(chunk) == maxBatchWriteItems || row == rows-1 {
			written, err := d.batchWrite(ctx, table, chunk)
			if err != nil {
				return totalWritten, err
			}
			totalWritten += written
			chunk = chunk[:0]
		}
	}

	return totalWritten, nil
}

func (d *DynamoDBDestination) batchWrite(ctx context.Context, table string, items []types.WriteRequest) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}

	input := &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{
			table: items,
		},
	}

	for attempt := 0; attempt < 5; attempt++ {
		output, err := d.client.BatchWriteItem(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("BatchWriteItem failed: %w", err)
		}

		unprocessed := output.UnprocessedItems[table]
		if len(unprocessed) == 0 {
			return int64(len(items)), nil
		}

		input.RequestItems = map[string][]types.WriteRequest{
			table: unprocessed,
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * 100 * time.Millisecond):
		}
	}

	return 0, fmt.Errorf("failed to write all items after retries, %d unprocessed", len(input.RequestItems[table]))
}

func (d *DynamoDBDestination) SwapTable(_ context.Context, _, _ string) error {
	return errors.New("dynamodb destination does not support atomic swap")
}

func (d *DynamoDBDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	// DynamoDB PutItem is inherently an upsert — we scan the staging table and put all items into the target.
	paginator := dynamodb.NewScanPaginator(d.client, &dynamodb.ScanInput{
		TableName: aws.String(opts.StagingTable),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to scan staging table %q: %w", opts.StagingTable, err)
		}

		chunk := make([]types.WriteRequest, 0, maxBatchWriteItems)
		for _, item := range page.Items {
			chunk = append(chunk, types.WriteRequest{
				PutRequest: &types.PutRequest{Item: item},
			})
			if len(chunk) == maxBatchWriteItems {
				if _, err := d.batchWrite(ctx, opts.TargetTable, chunk); err != nil {
					return err
				}
				chunk = chunk[:0]
			}
		}
		if len(chunk) > 0 {
			if _, err := d.batchWrite(ctx, opts.TargetTable, chunk); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d *DynamoDBDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for dynamodb destination")
}

func (d *DynamoDBDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for dynamodb destination")
}

func (d *DynamoDBDestination) DropTable(ctx context.Context, table string) error {
	return d.deleteTableIfExists(ctx, table)
}

func (d *DynamoDBDestination) Exec(_ context.Context, _ string, _ ...interface{}) error {
	return errors.New("exec is not supported for dynamodb destination")
}

func (d *DynamoDBDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for dynamodb destination")
}

func (d *DynamoDBDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *DynamoDBDestination) GetScheme() string                  { return "dynamodb" }
func (d *DynamoDBDestination) SupportsReplaceStrategy() bool      { return true }
func (d *DynamoDBDestination) SupportsAppendStrategy() bool       { return true }
func (d *DynamoDBDestination) SupportsMergeStrategy() bool        { return true }
func (d *DynamoDBDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *DynamoDBDestination) SupportsSCD2Strategy() bool         { return false }
func (d *DynamoDBDestination) SupportsAtomicSwap() bool           { return false }

func arrowToDynamoDB(arr arrow.Array, idx int) types.AttributeValue {
	if arr.IsNull(idx) {
		return &types.AttributeValueMemberNULL{Value: true}
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			val := arrowutil.Value(arr, idx)
			str, ok := val.(string)
			if !ok || str == "" {
				return &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", val)}
			}
			return &types.AttributeValueMemberS{Value: str}
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return &types.AttributeValueMemberBOOL{Value: a.Value(idx)}
	case *array.Int8:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Int16:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Int32:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Int64:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Uint8:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Uint16:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Uint32:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Uint64:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", a.Value(idx))}
	case *array.Float32:
		v := a.Value(idx)
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || !isDynamoDBNumberSafe(float64(v)) {
			return &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", v)}
		}
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%g", v)}
	case *array.Float64:
		v := a.Value(idx)
		if math.IsNaN(v) || math.IsInf(v, 0) || !isDynamoDBNumberSafe(v) {
			return &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", v)}
		}
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%g", v)}
	case *array.String:
		return &types.AttributeValueMemberS{Value: a.Value(idx)}
	case *array.LargeString:
		return &types.AttributeValueMemberS{Value: a.Value(idx)}
	case *array.Binary:
		v := a.Value(idx)
		if len(v) == 0 {
			return &types.AttributeValueMemberNULL{Value: true}
		}
		return &types.AttributeValueMemberB{Value: v}
	case *array.LargeBinary:
		v := a.Value(idx)
		if len(v) == 0 {
			return &types.AttributeValueMemberNULL{Value: true}
		}
		return &types.AttributeValueMemberB{Value: v}
	case *array.Decimal128:
		val := a.Value(idx)
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			return &types.AttributeValueMemberN{Value: val.ToString(dt.Scale)}
		}
		return &types.AttributeValueMemberN{Value: val.ToString(0)}
	case *array.Date32:
		return &types.AttributeValueMemberS{Value: a.Value(idx).ToTime().Format("2006-01-02")}
	case *array.Date64:
		return &types.AttributeValueMemberS{Value: a.Value(idx).ToTime().Format("2006-01-02")}
	case *array.Time64:
		raw := int64(a.Value(idx))
		unit := a.DataType().(*arrow.Time64Type).Unit
		var micros int64
		if unit == arrow.Nanosecond {
			micros = raw / 1000
		} else {
			micros = raw
		}
		h := micros / 3600000000
		micros %= 3600000000
		m := micros / 60000000
		micros %= 60000000
		s := micros / 1000000
		micros %= 1000000
		return &types.AttributeValueMemberS{Value: fmt.Sprintf("%02d:%02d:%02d.%06d", h, m, s, micros)}
	case *array.Timestamp:
		return &types.AttributeValueMemberS{Value: a.Value(idx).ToTime(a.DataType().(*arrow.TimestampType).Unit).Format(time.RFC3339Nano)}
	case *array.Struct:
		structType := a.DataType().(*arrow.StructType)
		fields := structType.Fields()
		m := make(map[string]types.AttributeValue, len(fields))
		for i, field := range fields {
			val := arrowToDynamoDB(a.Field(i), idx)
			if val != nil {
				m[field.Name] = val
			}
		}
		return &types.AttributeValueMemberM{Value: m}
	case array.ListLike:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		list := make([]types.AttributeValue, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			val := arrowToDynamoDB(values, i)
			if val != nil {
				list = append(list, val)
			}
		}
		return &types.AttributeValueMemberL{Value: list}
	case array.ExtensionArray:
		return arrowToDynamoDB(a.Storage(), idx)
	default:
		val := arrowutil.Value(arr, idx)
		if val == nil {
			return &types.AttributeValueMemberNULL{Value: true}
		}
		b, err := json.Marshal(val)
		if err != nil {
			return &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", val)}
		}
		return &types.AttributeValueMemberS{Value: string(b)}
	}
}

// isDynamoDBNumberSafe checks whether a float value is within DynamoDB's supported
// number range (magnitude up to ~10^125). Values outside this range must be stored
// as strings to avoid a ValidationException.
func isDynamoDBNumberSafe(v float64) bool {
	abs := math.Abs(v)
	return abs == 0 || abs < 1e126
}

var _ destination.Destination = (*DynamoDBDestination)(nil)
