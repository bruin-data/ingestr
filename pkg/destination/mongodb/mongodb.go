package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDBDestination struct {
	client   *mongo.Client
	database string
	uri      string
}

func NewMongoDBDestination() *MongoDBDestination {
	return &MongoDBDestination{}
}

func (d *MongoDBDestination) Schemes() []string {
	return []string{"mongodb", "mongodb+srv"}
}

func (d *MongoDBDestination) Connect(ctx context.Context, uri string) error {
	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	d.client = client
	d.database = extractDatabase(uri)
	d.uri = uri
	config.Debug("[MONGODB DEST] Connected to server")
	return nil
}

func (d *MongoDBDestination) Close(ctx context.Context) error {
	if d.client != nil {
		return d.client.Disconnect(ctx)
	}
	return nil
}

func (d *MongoDBDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	resolvedDB, collectionName := resolveDatabaseAndCollection(d.database, opts.Table)
	if collectionName == "" {
		return fmt.Errorf("collection name is required")
	}

	if opts.DropFirst {
		collection := d.client.Database(resolvedDB).Collection(collectionName)
		if err := collection.Drop(ctx); err != nil && !isNamespaceNotFound(err) {
			return fmt.Errorf("failed to drop collection: %w", err)
		}
	}

	return nil
}

func (d *MongoDBDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[MONGODB DEST] Waiting for records...")
	startTotal := time.Now()

	collection, err := d.getCollection(opts.Table)
	if err != nil {
		return err
	}

	batchNum := 0
	var totalRows int64

	for result := range records {
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
		rows, err := d.writeBatch(ctx, collection, record)
		record.Release()
		if err != nil {
			return fmt.Errorf("failed to insert batch: %w", err)
		}

		totalRows += rows
		config.Debug("[MONGODB DEST] Batch %d: %d docs in %v (%.0f docs/sec)", batchNum, rows, time.Since(startBatch), float64(rows)/time.Since(startBatch).Seconds())
	}

	config.Debug("[MONGODB DEST] Total: %d docs written in %v (%.0f docs/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *MongoDBDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	config.Debug("[MONGODB DEST] Starting parallel write with %d workers", parallelism)
	startTotal := time.Now()

	collection, err := d.getCollection(opts.Table)
	if err != nil {
		return err
	}

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
				myBatch := int(atomic.AddInt64(&batchNum, 1))
				if result.Err != nil {
					results <- writeResult{batchNum: myBatch, err: result.Err}
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
				rows, err := d.writeBatch(ctx, collection, record)
				record.Release()

				results <- writeResult{
					batchNum: myBatch,
					rows:     rows,
					duration: time.Since(startBatch),
					err:      err,
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
			config.Debug("[MONGODB DEST] Worker error on batch %d: %v", res.batchNum, res.err)
			continue
		}
		if res.err == nil {
			totalRows += res.rows
			config.Debug("[MONGODB DEST] Batch %d: %d docs in %v (%.0f docs/sec)", res.batchNum, res.rows, res.duration, float64(res.rows)/res.duration.Seconds())
		}
	}

	if firstErr != nil {
		return fmt.Errorf("parallel write failed: %w", firstErr)
	}

	config.Debug("[MONGODB DEST] Total: %d docs written in %v (%.0f docs/sec)", totalRows, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())
	return nil
}

func (d *MongoDBDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return errors.New("mongo destination does not support atomic swap")
}

func (d *MongoDBDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("merge requires at least one primary key")
	}

	stagingCol, err := d.getCollection(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("failed to get staging collection: %w", err)
	}

	targetCol, err := d.getCollection(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("failed to get target collection: %w", err)
	}

	isCDC := destination.HasCDCDeletedColumn(opts.Columns)
	findOpts := options.Find()
	if isCDC {
		// Process changes in LSN order so the per-PK composition below sees the
		// latest change last (LSN strings are fixed-width and sort
		// lexicographically). Deletes sort after other changes with the same
		// LSN so a tied delete wins, mirroring destination.CDCLatestOverallOrderBy.
		findOpts.SetSort(bson.D{{Key: destination.CDCLSNColumn, Value: 1}, {Key: destination.CDCDeletedColumn, Value: 1}})
	}

	cursor, err := stagingCol.Find(ctx, bson.D{}, findOpts)
	if err != nil {
		return fmt.Errorf("failed to read staging collection: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	const batchSize = 1000
	var operations []mongo.WriteModel
	var totalUpserted int64

	// For CDC, compose one document per PK: row data comes from the latest
	// non-deleted change (so a trailing delete keeps the last update's values),
	// CDC columns and the deleted flag from the latest change overall.
	type cdcComposed struct {
		filter bson.M
		latest bson.M
		active bson.M
	}
	var composedOrder []string
	composedByPK := map[string]*cdcComposed{}

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return fmt.Errorf("failed to decode staging document: %w", err)
		}

		filter := bson.M{}
		for _, pk := range opts.PrimaryKeys {
			filter[pk] = doc[pk]
		}

		delete(doc, "_id")

		if isCDC {
			key := fmt.Sprintf("%v", filter)
			entry, ok := composedByPK[key]
			if !ok {
				entry = &cdcComposed{filter: filter}
				composedByPK[key] = entry
				composedOrder = append(composedOrder, key)
			}
			entry.latest = doc
			if deleted, _ := doc[destination.CDCDeletedColumn].(bool); !deleted {
				entry.active = doc
			}
			continue
		}

		operations = append(operations, mongo.NewReplaceOneModel().
			SetFilter(filter).
			SetReplacement(doc).
			SetUpsert(true))

		if len(operations) >= batchSize {
			if _, err := targetCol.BulkWrite(ctx, operations, options.BulkWrite().SetOrdered(false)); err != nil {
				return fmt.Errorf("failed to bulk write merge batch: %w", err)
			}
			totalUpserted += int64(len(operations))
			operations = operations[:0]
		}
	}

	if err := cursor.Err(); err != nil {
		return fmt.Errorf("staging cursor error: %w", err)
	}

	for _, key := range composedOrder {
		entry := composedByPK[key]
		doc := entry.active
		if doc == nil {
			// Delete-only window: update CDC columns on an existing document
			// without clobbering row data; unknown rows are not materialized
			// from a bare delete image.
			operations = append(operations, mongo.NewUpdateOneModel().
				SetFilter(entry.filter).
				SetUpdate(bson.M{"$set": bson.M{
					destination.CDCDeletedColumn:  entry.latest[destination.CDCDeletedColumn],
					destination.CDCLSNColumn:      entry.latest[destination.CDCLSNColumn],
					destination.CDCSyncedAtColumn: entry.latest[destination.CDCSyncedAtColumn],
				}}))
		} else {
			doc[destination.CDCDeletedColumn] = entry.latest[destination.CDCDeletedColumn]
			doc[destination.CDCLSNColumn] = entry.latest[destination.CDCLSNColumn]
			doc[destination.CDCSyncedAtColumn] = entry.latest[destination.CDCSyncedAtColumn]
			operations = append(operations, mongo.NewReplaceOneModel().
				SetFilter(entry.filter).
				SetReplacement(doc).
				SetUpsert(true))
		}

		if len(operations) >= batchSize {
			if _, err := targetCol.BulkWrite(ctx, operations, options.BulkWrite().SetOrdered(false)); err != nil {
				return fmt.Errorf("failed to bulk write merge batch: %w", err)
			}
			totalUpserted += int64(len(operations))
			operations = operations[:0]
		}
	}

	if len(operations) > 0 {
		if _, err := targetCol.BulkWrite(ctx, operations, options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("failed to bulk write final merge batch: %w", err)
		}
		totalUpserted += int64(len(operations))
	}

	config.Debug("[MONGODB DEST] Merge complete: %d documents upserted into %s", totalUpserted, opts.TargetTable)
	return nil
}

func (d *MongoDBDestination) SupportsCDCMerge() bool { return true }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the collection for CDC resume.
func (d *MongoDBDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	collection, err := d.getCollection(table)
	if err != nil {
		return "", err
	}

	opts := options.FindOne().SetSort(bson.D{{Key: destination.CDCLSNColumn, Value: -1}}).SetProjection(bson.M{destination.CDCLSNColumn: 1})
	var doc bson.M
	if err := collection.FindOne(ctx, bson.M{}, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return "", nil
		}
		return "", err
	}
	if lsn, ok := doc[destination.CDCLSNColumn].(string); ok {
		return lsn, nil
	}
	return "", nil
}

func (d *MongoDBDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for mongo destination")
}

func (d *MongoDBDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for mongo destination")
}

func (d *MongoDBDestination) DropTable(ctx context.Context, table string) error {
	collection, err := d.getCollection(table)
	if err != nil {
		return err
	}
	if err := collection.Drop(ctx); err != nil && !isNamespaceNotFound(err) {
		return fmt.Errorf("failed to drop collection: %w", err)
	}
	return nil
}

func (d *MongoDBDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return errors.New("exec is not supported for mongo destination")
}

func (d *MongoDBDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for mongo destination")
}

func (d *MongoDBDestination) SupportsReplaceStrategy() bool { return true }

func (d *MongoDBDestination) SupportsAppendStrategy() bool { return true }

func (d *MongoDBDestination) SupportsMergeStrategy() bool { return true }

func (d *MongoDBDestination) SupportsDeleteInsertStrategy() bool { return false }

func (d *MongoDBDestination) SupportsSCD2Strategy() bool { return false }

func (d *MongoDBDestination) SupportsAtomicSwap() bool { return false }

func (d *MongoDBDestination) GetScheme() string { return "mongodb" }

func (d *MongoDBDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *MongoDBDestination) getCollection(table string) (*mongo.Collection, error) {
	resolvedDB, collectionName := resolveDatabaseAndCollection(d.database, table)
	if resolvedDB == "" || collectionName == "" {
		return nil, fmt.Errorf("invalid destination table: %q", table)
	}
	return d.client.Database(resolvedDB).Collection(collectionName), nil
}

func (d *MongoDBDestination) writeBatch(ctx context.Context, collection *mongo.Collection, record arrow.RecordBatch) (int64, error) {
	rows := int(record.NumRows())
	cols := int(record.NumCols())
	if rows == 0 {
		return 0, nil
	}

	columns := make([]string, cols)
	for i := 0; i < cols; i++ {
		columns[i] = record.ColumnName(i)
	}

	docs := make([]interface{}, 0, rows)
	for row := 0; row < rows; row++ {
		doc := bson.M{}
		for col := 0; col < cols; col++ {
			val, err := arrowValueToBSON(record.Column(col), row)
			if err != nil {
				return 0, err
			}
			doc[columns[col]] = val
		}
		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return 0, nil
	}

	if _, err := collection.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false)); err != nil {
		return int64(len(docs)), err
	}

	return int64(len(docs)), nil
}

func arrowValueToBSON(arr arrow.Array, idx int) (interface{}, error) {
	if arr.IsNull(idx) {
		return nil, nil
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			val := arrowutil.Value(arr, idx)
			str, ok := val.(string)
			if !ok || str == "" {
				return val, nil
			}
			var decoded interface{}
			if err := json.Unmarshal([]byte(str), &decoded); err != nil {
				return str, nil
			}
			return decoded, nil
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx), nil
	case *array.Int8:
		return int64(a.Value(idx)), nil
	case *array.Int16:
		return int64(a.Value(idx)), nil
	case *array.Int32:
		return int64(a.Value(idx)), nil
	case *array.Int64:
		return a.Value(idx), nil
	case *array.Uint8:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint16:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint32:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint64:
		return convertUint(a.Value(idx))
	case *array.Float32:
		return float64(a.Value(idx)), nil
	case *array.Float64:
		return a.Value(idx), nil
	case *array.String:
		return a.Value(idx), nil
	case *array.LargeString:
		return a.Value(idx), nil
	case *array.Binary:
		return a.Value(idx), nil
	case *array.LargeBinary:
		return a.Value(idx), nil
	case *array.Decimal128:
		val := a.Value(idx)
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			decStr := val.ToString(dt.Scale)
			dec, err := primitive.ParseDecimal128(decStr)
			if err == nil {
				return dec, nil
			}
			return decStr, nil
		}
		return val.ToString(0), nil
	case *array.Date32:
		return a.Value(idx).ToTime(), nil
	case *array.Date64:
		return a.Value(idx).ToTime(), nil
	case *array.Time64:
		return formatTime64(a.Value(idx)), nil
	case *array.Timestamp:
		return a.Value(idx).ToTime(arrow.Microsecond), nil
	case *array.Struct:
		structType := a.DataType().(*arrow.StructType)
		fields := structType.Fields()
		result := bson.M{}
		for i, field := range fields {
			val, err := arrowValueToBSON(a.Field(i), idx)
			if err != nil {
				return nil, err
			}
			result[field.Name] = val
		}
		return result, nil
	case array.ListLike:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		list := make([]interface{}, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			val, err := arrowValueToBSON(values, i)
			if err != nil {
				return nil, err
			}
			list = append(list, val)
		}
		return list, nil
	case array.ExtensionArray:
		return arrowutil.Value(a.Storage(), idx), nil
	default:
		return arrowutil.Value(arr, idx), nil
	}
}

func formatTime64(val arrow.Time64) string {
	micros := int64(val)
	if micros < 0 {
		micros = -micros
	}
	h := micros / 3600000000
	micros %= 3600000000
	m := micros / 60000000
	micros %= 60000000
	s := micros / 1000000
	micros %= 1000000
	return fmt.Sprintf("%02d:%02d:%02d.%06d", h, m, s, micros)
}

func convertUint(v uint64) (interface{}, error) {
	if v <= math.MaxInt64 {
		return int64(v), nil
	}
	return fmt.Sprintf("%d", v), nil
}

func extractDatabase(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}

	path := strings.TrimPrefix(u.Path, "/")
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}

	return path
}

func resolveDatabaseAndCollection(defaultDB, table string) (string, string) {
	if table == "" {
		return defaultDB, ""
	}

	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}

	// Plain collection name — fall back to URI database
	if defaultDB != "" {
		return defaultDB, table
	}

	return "", table
}

func isNamespaceNotFound(err error) bool {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		if cmdErr.Code == 26 || cmdErr.Name == "NamespaceNotFound" {
			return true
		}
	}
	return false
}

var _ destination.Destination = (*MongoDBDestination)(nil)
