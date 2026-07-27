package mongodb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
)

const defaultBatchSize = 10000

type MongoDBSource struct {
	client   *mongo.Client
	database string
	uri      string
}

func NewMongoDBSource() *MongoDBSource {
	return &MongoDBSource{}
}

func (s *MongoDBSource) Schemes() []string {
	return []string{"mongodb", "mongodb+srv"}
}

func (s *MongoDBSource) Connect(ctx context.Context, uri string) error {
	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	s.client = client
	s.database = extractDatabase(uri)
	s.uri = uri
	config.Debug("[MONGODB] Connected to database: %s", s.database)
	return nil
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

func (s *MongoDBSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Disconnect(ctx)
	}
	return nil
}

func (s *MongoDBSource) HandlesIncrementality() bool {
	return false
}

func (s *MongoDBSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = []string{"_id"}
	}

	return &source.DynamicSourceTable{
		TableName:                        tableName,
		TablePrimaryKeys:                 pks,
		TableIncrementalKey:              req.IncrementalKey,
		TableStrategy:                    strategy,
		TableSupportsExtractPartitioning: true,
		KnownSchema:                      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("MongoDB does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

// parseTableSpec parses the table string into database, collection, and optional custom query.
//   - "database.collection"            → db from table name (required for regular path)
//   - "database.collection:[pipeline]" → db from table name
//   - "collection:[pipeline]"          → db from URI (fallback for custom query path only)
func (s *MongoDBSource) parseTableSpec(table string) (db string, col string, customQuery []bson.M, err error) {
	var collectionPart string
	var queryJSON string

	hasCustomQuery := false
	if idx := strings.Index(table, ":"); idx >= 0 {
		collectionPart = table[:idx]
		queryJSON = table[idx+1:]
		hasCustomQuery = true
	} else {
		collectionPart = table
	}

	parts := strings.SplitN(collectionPart, ".", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		db = parts[0]
		col = parts[1]
	} else if hasCustomQuery {
		// Custom query path: fall back to URI database
		db = s.database
		col = collectionPart
		if db == "" {
			return "", "", nil, fmt.Errorf("database not specified: provide it in the URI or use database.collection format in source_table")
		}
		if col == "" {
			return "", "", nil, fmt.Errorf("collection name is empty")
		}
	} else {
		// Regular path: database.collection format is required
		return "", "", nil, fmt.Errorf("source_table must be in the format database.collection, got %q", collectionPart)
	}

	if queryJSON != "" {
		converted := convertMongoShellToExtendedJSON(queryJSON)
		var pipeline []bson.M
		if err := json.Unmarshal([]byte(converted), &pipeline); err != nil {
			return "", "", nil, fmt.Errorf("invalid aggregation pipeline JSON: %w", err)
		}
		if len(pipeline) == 0 {
			return "", "", nil, fmt.Errorf("aggregation pipeline must not be empty")
		}
		for i, stage := range pipeline {
			pipeline[i] = resolveExtendedJSON(stage)
		}
		customQuery = pipeline
	}

	return db, col, customQuery, nil
}

var shellPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`ISODate\("([^"]+)"\)`), `{"$$date": "$1"}`},
	{regexp.MustCompile(`ObjectId\("([^"]+)"\)`), `{"$$oid": "$1"}`},
	{regexp.MustCompile(`NumberLong\("([^"]+)"\)`), `{"$$numberLong": "$1"}`},
	{regexp.MustCompile(`NumberLong\(([^)]+)\)`), `{"$$numberLong": "$1"}`},
	{regexp.MustCompile(`NumberInt\("([^"]+)"\)`), `{"$$numberInt": "$1"}`},
	{regexp.MustCompile(`NumberInt\(([^)]+)\)`), `{"$$numberInt": "$1"}`},
	{regexp.MustCompile(`NumberDecimal\("([^"]+)"\)`), `{"$$numberDecimal": "$1"}`},
	{regexp.MustCompile(`Timestamp\((\d+),\s*(\d+)\)`), `{"$$timestamp": {"t": $1, "i": $2}}`},
	{regexp.MustCompile(`BinData\((\d+),\s*"([^"]+)"\)`), `{"$$binary": {"base64": "$2", "subType": "$1"}}`},
	{regexp.MustCompile(`MinKey\(\)`), `{"$$minKey": 1}`},
	{regexp.MustCompile(`MaxKey\(\)`), `{"$$maxKey": 1}`},
	{regexp.MustCompile(`UUID\("([^"]+)"\)`), `{"$$uuid": "$1"}`},
	{regexp.MustCompile(`DBRef\("([^"]+)",\s*"([^"]+)"\)`), `{"$$ref": "$1", "$$id": "$2"}`},
	{regexp.MustCompile(`Code\("([^"]+)"\)`), `{"$$code": "$1"}`},
}

// resolveExtendedJSON recursively walks a bson.M and converts Extended JSON type wrappers
// (e.g. {"$date": "..."}, {"$oid": "..."}) into actual BSON primitive types that the
// MongoDB driver understands.
func resolveExtendedJSON(m bson.M) bson.M {
	result := make(bson.M, len(m))
	for k, v := range m {
		result[k] = resolveExtendedJSONValue(v)
	}
	return result
}

func resolveExtendedJSONValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		if len(val) == 1 {
			if s, ok := val["$date"].(string); ok {
				for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02"} {
					if t, err := time.Parse(layout, s); err == nil {
						return primitive.NewDateTimeFromTime(t)
					}
				}
			}
			if s, ok := val["$oid"].(string); ok {
				oid, err := primitive.ObjectIDFromHex(s)
				if err == nil {
					return oid
				}
			}
			if s, ok := val["$numberLong"].(string); ok {
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					return n
				}
				return s
			}
			if s, ok := val["$numberInt"].(string); ok {
				if n, err := strconv.ParseInt(s, 10, 32); err == nil {
					return int32(n)
				}
				return s
			}
			if s, ok := val["$numberDecimal"].(string); ok {
				d, err := primitive.ParseDecimal128(s)
				if err == nil {
					return d
				}
			}
			if _, ok := val["$minKey"]; ok {
				return primitive.MinKey{}
			}
			if _, ok := val["$maxKey"]; ok {
				return primitive.MaxKey{}
			}
			if s, ok := val["$uuid"].(string); ok {
				return s
			}
			if s, ok := val["$code"].(string); ok {
				return primitive.JavaScript(s)
			}
		}
		if len(val) == 2 {
			if ts, ok := val["$timestamp"].(map[string]any); ok {
				t, tOk := ts["t"].(float64)
				i, iOk := ts["i"].(float64)
				if tOk && iOk {
					return primitive.Timestamp{T: uint32(t), I: uint32(i)}
				}
			}
			if bin, ok := val["$binary"].(map[string]any); ok {
				b64, _ := bin["base64"].(string)
				subType, _ := bin["subType"].(string)
				st := byte(0)
				if len(subType) > 0 {
					if v, err := strconv.ParseUint(subType, 16, 8); err == nil {
						st = byte(v)
					}
				}
				data, err := base64.StdEncoding.DecodeString(b64)
				if err == nil {
					return primitive.Binary{Subtype: st, Data: data}
				}
			}
			if ref, ok := val["$ref"]; ok {
				if id, ok2 := val["$id"]; ok2 {
					return bson.M{"$ref": ref, "$id": id}
				}
			}
		}
		// Not an Extended JSON type — recurse into it
		resolved := make(bson.M, len(val))
		for k, v2 := range val {
			resolved[k] = resolveExtendedJSONValue(v2)
		}
		return resolved
	case bson.M:
		return resolveExtendedJSON(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = resolveExtendedJSONValue(item)
		}
		return result
	default:
		return val
	}
}

// convertMongoShellToExtendedJSON converts MongoDB shell syntax to Extended JSON v2.
func convertMongoShellToExtendedJSON(s string) string {
	for _, p := range shellPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// validateIncrementalKeyProjection checks that the incremental key is included in any $project
// stage in the aggregation pipeline.
func validateIncrementalKeyProjection(pipeline []bson.M, incrementalKey string) error {
	for _, stage := range pipeline {
		projectStage, ok := stage["$project"]
		if !ok {
			continue
		}

		var proj map[string]any
		switch p := projectStage.(type) {
		case bson.M:
			proj = p
		case map[string]any:
			proj = p
		default:
			continue
		}

		// Check if this is an inclusion projection (has any field set to 1/true)
		isInclusion := false
		for k, v := range proj {
			if k == "_id" {
				continue
			}
			if isIncludeValue(v) {
				isInclusion = true
				break
			}
		}

		if !isInclusion {
			// Exclusion projection — all fields are included by default unless explicitly excluded
			continue
		}

		// Inclusion projection — incremental key must be explicitly listed
		if _, exists := proj[incrementalKey]; !exists {
			return fmt.Errorf("incremental key %q must be included in the $project stage of the aggregation pipeline", incrementalKey)
		}
	}

	return nil
}

func isIncludeValue(v any) bool {
	switch val := v.(type) {
	case float64:
		return val == 1
	case int:
		return val == 1
	case int32:
		return val == 1
	case int64:
		return val == 1
	case bool:
		return val
	default:
		return false
	}
}

// substituteIntervalParams replaces ":interval_start" and ":interval_end" string placeholders
// in a custom aggregation pipeline with actual time values.
func substituteIntervalParams(pipeline []bson.M, intervalStart, intervalEnd *time.Time) []bson.M {
	if intervalStart == nil && intervalEnd == nil {
		return pipeline
	}

	result := make([]bson.M, len(pipeline))
	for i, stage := range pipeline {
		result[i] = replacePlaceholders(stage, intervalStart, intervalEnd)
	}
	return result
}

func replacePlaceholders(m bson.M, intervalStart, intervalEnd *time.Time) bson.M {
	result := make(bson.M, len(m))
	for k, v := range m {
		result[k] = replaceValue(v, intervalStart, intervalEnd)
	}
	return result
}

func replaceValue(v any, intervalStart, intervalEnd *time.Time) any {
	switch val := v.(type) {
	case string:
		if val == ":interval_start" && intervalStart != nil {
			return primitive.NewDateTimeFromTime(*intervalStart)
		}
		if val == ":interval_end" && intervalEnd != nil {
			return primitive.NewDateTimeFromTime(*intervalEnd)
		}
		return val
	case bson.M:
		return replacePlaceholders(val, intervalStart, intervalEnd)
	case map[string]any:
		result := make(bson.M, len(val))
		for k, v2 := range val {
			result[k] = replaceValue(v2, intervalStart, intervalEnd)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = replaceValue(item, intervalStart, intervalEnd)
		}
		return result
	default:
		return val
	}
}

func (s *MongoDBSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()

	db, col, customQuery, err := s.parseTableSpec(table)
	if err != nil {
		return nil, fmt.Errorf("failed to parse table spec: %w", err)
	}
	config.Debug("[MONGODB] Starting read from %s.%s", db, col)

	dbNames, err := s.client.ListDatabaseNames(ctx, bson.M{"name": db})
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}
	if len(dbNames) == 0 {
		return nil, fmt.Errorf("database %q not found", db)
	}

	collections, err := s.client.Database(db).ListCollectionNames(ctx, bson.M{"name": col})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections in database %q: %w", db, err)
	}
	if len(collections) == 0 {
		return nil, fmt.Errorf("collection %q not found in database %q", col, db)
	}

	collection := s.client.Database(db).Collection(col)

	batchSize := normalizeBatchSize(opts.PageSize)
	if opts.ExtractPartitioningEnabled() {
		if customQuery != nil {
			return nil, fmt.Errorf("MongoDB aggregation pipelines do not support extract partitioning")
		}
		if opts.Limit > 0 {
			return nil, fmt.Errorf("MongoDB extract partitioning cannot be combined with a row limit")
		}
		partitionSchema := &schema.TableSchema{Columns: []schema.Column{{
			Name:     opts.ExtractPartitionBy,
			DataType: schema.TypeInt64,
		}}}
		return source.ReadExtractPartitions(
			ctx,
			opts,
			partitionSchema,
			func(ctx context.Context, windowOpts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readFind(ctx, collection, batchSize, windowOpts, startTotal)
			},
			func(ctx context.Context, boundsOpts source.ReadOptions) (source.ExtractPartitionBounds, error) {
				return discoverMongoNumericPartitionBounds(ctx, collection, boundsOpts.ExtractPartitionBy)
			},
		)
	}

	if customQuery != nil {
		if opts.IncrementalKey != "" {
			if err := validateIncrementalKeyProjection(customQuery, opts.IncrementalKey); err != nil {
				return nil, err
			}
		}
		return s.readAggregate(ctx, collection, customQuery, batchSize, opts, startTotal)
	}

	return s.readFind(ctx, collection, batchSize, opts, startTotal)
}

func (s *MongoDBSource) readFind(ctx context.Context, collection *mongo.Collection, batchSize int, opts source.ReadOptions, startTotal time.Time) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 5)

	go func() {
		defer close(results)

		findOpts := options.Find()
		if opts.Limit > 0 {
			findOpts.SetLimit(int64(opts.Limit))
		}
		findOpts.SetBatchSize(int32(batchSize))

		filters := make([]bson.D, 0, 2)
		if opts.IncrementalKey != "" {
			if incrementalFilter := mongoIncrementalFilter(opts); len(incrementalFilter) > 0 {
				filters = append(filters, incrementalFilter)
			}

			findOpts.SetSort(bson.D{{Key: opts.IncrementalKey, Value: 1}})
		}
		if partitionFilter := mongoExtractPartitionFilter(opts); len(partitionFilter) > 0 {
			filters = append(filters, partitionFilter)
		}

		filter := bson.D{}
		switch len(filters) {
		case 1:
			filter = filters[0]
		case 2:
			filter = bson.D{{Key: "$and", Value: bson.A{filters[0], filters[1]}}}
		}

		cursor, err := collection.Find(ctx, filter, findOpts)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query MongoDB: %w", err)}
			return
		}
		defer func() { _ = cursor.Close(ctx) }()

		s.consumeCursor(ctx, cursor, batchSize, opts, results, startTotal)
	}()

	return results, nil
}

func mongoIncrementalFilter(opts source.ReadOptions) bson.D {
	if opts.IncrementalKey == "" || opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return nil
	}

	rangeFilter := bson.D{}
	if opts.IntervalStart != nil {
		rangeFilter = append(rangeFilter, bson.E{Key: "$gte", Value: opts.IntervalStart})
	}
	if opts.IntervalEnd != nil {
		rangeFilter = append(rangeFilter, bson.E{Key: "$lte", Value: opts.IntervalEnd})
	}
	return bson.D{{Key: opts.IncrementalKey, Value: rangeFilter}}
}

func mongoExtractPartitionFilter(opts source.ReadOptions) bson.D {
	if opts.ExtractPartitionBy == "" {
		return nil
	}
	if opts.ExtractPartitionIsNull {
		return bson.D{{Key: opts.ExtractPartitionBy, Value: nil}}
	}
	if opts.ExtractPartitionKind != source.ExtractPartitionKindNumeric || opts.ExtractPartitionNumericStart == nil || opts.ExtractPartitionNumericEnd == nil {
		return nil
	}

	if opts.ExtractPartitionEndInclusive {
		return bson.D{{Key: opts.ExtractPartitionBy, Value: bson.D{
			{Key: "$gte", Value: *opts.ExtractPartitionNumericStart},
		}}}
	}
	return bson.D{{Key: opts.ExtractPartitionBy, Value: bson.D{
		{Key: "$gte", Value: *opts.ExtractPartitionNumericStart},
		{Key: "$lt", Value: *opts.ExtractPartitionNumericEnd},
	}}}
}

func discoverMongoNumericPartitionBounds(ctx context.Context, collection *mongo.Collection, field string) (source.ExtractPartitionBounds, error) {
	indexed, err := mongoCollectionHasLeadingIndex(ctx, collection, field)
	if err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to inspect MongoDB indexes: %w", err)
	}
	if !indexed {
		return source.ExtractPartitionBounds{}, fmt.Errorf("MongoDB extract partition column %q must be the leading field of an index", field)
	}

	numericTypeFilter := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: field, Value: bson.D{{Key: "$type", Value: "int"}}}},
		bson.D{{Key: field, Value: bson.D{{Key: "$type", Value: "long"}}}},
	}}}
	unsupportedFilter := bson.D{
		{Key: field, Value: bson.D{{Key: "$exists", Value: true}, {Key: "$ne", Value: nil}}},
		{Key: "$nor", Value: bson.A{
			bson.D{{Key: field, Value: bson.D{{Key: "$type", Value: "int"}}}},
			bson.D{{Key: field, Value: bson.D{{Key: "$type", Value: "long"}}}},
		}},
	}
	if err := collection.FindOne(ctx, unsupportedFilter, options.FindOne().SetProjection(bson.D{{Key: "_id", Value: 1}})).Err(); err == nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("MongoDB extract partition column %q contains non-integer values", field)
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to validate MongoDB extract partition values: %w", err)
	}

	minimum, found, err := findMongoNumericPartitionBound(ctx, collection, field, numericTypeFilter, 1)
	if err != nil {
		return source.ExtractPartitionBounds{}, err
	}
	nullErr := collection.FindOne(ctx, bson.D{{Key: field, Value: nil}}, options.FindOne().SetProjection(bson.D{{Key: "_id", Value: 1}})).Err()
	if nullErr != nil && !errors.Is(nullErr, mongo.ErrNoDocuments) {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to inspect null MongoDB extract partition values: %w", nullErr)
	}
	hasNulls := nullErr == nil
	if !found {
		return source.ExtractPartitionBounds{Kind: source.ExtractPartitionKindNumeric, HasNulls: hasNulls}, nil
	}
	maximum, found, err := findMongoNumericPartitionBound(ctx, collection, field, numericTypeFilter, -1)
	if err != nil {
		return source.ExtractPartitionBounds{}, err
	}
	if !found {
		return source.ExtractPartitionBounds{}, fmt.Errorf("MongoDB extract partition column %q has a minimum but no maximum", field)
	}
	return source.ExtractPartitionBounds{
		NumericStart: minimum,
		NumericEnd:   maximum,
		Kind:         source.ExtractPartitionKindNumeric,
		HasRange:     true,
		HasNulls:     hasNulls,
	}, nil
}

func findMongoNumericPartitionBound(ctx context.Context, collection *mongo.Collection, field string, filter bson.D, direction int) (int64, bool, error) {
	var result bson.Raw
	err := collection.FindOne(
		ctx,
		filter,
		options.FindOne().SetSort(bson.D{{Key: field, Value: direction}}).SetProjection(bson.D{{Key: field, Value: 1}, {Key: "_id", Value: 0}}),
	).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to discover MongoDB extract partition bounds: %w", err)
	}
	value := result.Lookup(field)
	switch value.Type {
	case bson.TypeInt32:
		v, ok := value.Int32OK()
		if !ok {
			return 0, false, fmt.Errorf("failed to decode MongoDB extract partition bound for %q", field)
		}
		return int64(v), true, nil
	case bson.TypeInt64:
		v, ok := value.Int64OK()
		if !ok {
			return 0, false, fmt.Errorf("failed to decode MongoDB extract partition bound for %q", field)
		}
		return v, true, nil
	default:
		return 0, false, fmt.Errorf("MongoDB extract partition column %q is not an integer", field)
	}
}

func mongoCollectionHasLeadingIndex(ctx context.Context, collection *mongo.Collection, field string) (bool, error) {
	specs, err := collection.Indexes().ListSpecifications(ctx)
	if err != nil {
		return false, err
	}
	for _, spec := range specs {
		elements, err := spec.KeysDocument.Elements()
		if err != nil {
			return false, err
		}
		if len(elements) > 0 && elements[0].Key() == field {
			return true, nil
		}
	}
	return false, nil
}

func (s *MongoDBSource) readAggregate(ctx context.Context, collection *mongo.Collection, pipeline []bson.M, batchSize int, opts source.ReadOptions, startTotal time.Time) (<-chan source.RecordBatchResult, error) {
	pipeline = substituteIntervalParams(pipeline, opts.IntervalStart, opts.IntervalEnd)

	config.Debug("[MONGODB] Running aggregation pipeline with %d stages", len(pipeline))

	results := make(chan source.RecordBatchResult, 5)

	go func() {
		defer close(results)

		aggOpts := options.Aggregate().
			SetAllowDiskUse(true).
			SetBatchSize(int32(batchSize))

		cursor, err := collection.Aggregate(ctx, pipeline, aggOpts)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to run aggregation: %w", err)}
			return
		}
		defer func() { _ = cursor.Close(ctx) }()

		s.consumeCursor(ctx, cursor, batchSize, opts, results, startTotal)
	}()

	return results, nil
}

func (s *MongoDBSource) consumeCursor(ctx context.Context, cursor *mongo.Cursor, batchSize int, opts source.ReadOptions, results chan<- source.RecordBatchResult, startTotal time.Time) {
	batchNum := 0
	totalRows := int64(0)
	mem := newRecyclingAllocator(memory.NewGoAllocator(), mongoArrowBufferCacheSize)

	for {
		select {
		case <-ctx.Done():
			results <- source.RecordBatchResult{Err: ctx.Err()}
			return
		default:
		}

		startBatch := time.Now()
		var builder mongoRecordBatchBuilder = newMongoRawBatchBuilderWithAllocator(mem, opts.ExcludeColumns, batchSize)
		if opts.Schema != nil {
			builder = newMongoSchemaBatchBuilderWithAllocator(mem, opts.Schema.Columns, opts.ExcludeColumns, batchSize)
		}
		batchRows := 0

		for batchRows < batchSize && cursor.Next(ctx) {
			if err := builder.AppendRawDocument(cursor.Current); err != nil {
				builder.Release()
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to build Arrow batch: %w", err)}
				return
			}
			batchRows++
		}

		if err := cursor.Err(); err != nil {
			builder.Release()
			results <- source.RecordBatchResult{Err: fmt.Errorf("cursor error: %w", err)}
			return
		}

		if batchRows == 0 {
			builder.Release()
			break
		}

		record, err := builder.NewRecordBatch()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
			return
		}

		batchNum++
		totalRows += int64(batchRows)
		config.Debug("[MONGODB] Batch %d: %d documents read in %v (total: %d)", batchNum, batchRows, time.Since(startBatch), totalRows)

		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[MONGODB] Total: %d documents in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
}

func convertBSONValue(val any) any {
	switch v := val.(type) {
	case bson.M:
		m := make(map[string]any, len(v))
		for key, value := range v {
			m[key] = convertBSONValue(value)
		}
		return m
	case primitive.ObjectID:
		return v.Hex()
	case primitive.DateTime:
		return v.Time()
	case primitive.Timestamp:
		return time.Unix(int64(v.T), 0)
	case primitive.Binary:
		return v.Data
	case primitive.Decimal128:
		return v.String()
	case primitive.Regex:
		return v.Pattern
	case bson.D:
		m := make(map[string]any, len(v))
		for _, e := range v {
			m[e.Key] = convertBSONValue(e.Value)
		}
		return m
	case primitive.A:
		result := make([]any, len(v))
		for i, elem := range v {
			result[i] = convertBSONValue(elem)
		}
		return result
	default:
		return val
	}
}

func convertRawBSONValue(val bson.RawValue) any {
	switch val.Type {
	case bson.TypeDouble:
		if v, ok := val.DoubleOK(); ok {
			return v
		}
	case bson.TypeString:
		if v, ok := val.StringValueOK(); ok {
			return v
		}
	case bson.TypeEmbeddedDocument:
		if doc, ok := val.DocumentOK(); ok {
			return convertRawDocument(doc)
		}
	case bson.TypeArray:
		if arr, ok := val.ArrayOK(); ok {
			return convertRawArray(arr)
		}
	case bson.TypeBinary:
		if _, data, ok := val.BinaryOK(); ok {
			return data
		}
	case bson.TypeObjectID:
		if v, ok := val.ObjectIDOK(); ok {
			return v.Hex()
		}
	case bson.TypeBoolean:
		if v, ok := val.BooleanOK(); ok {
			return v
		}
	case bson.TypeDateTime:
		if v, ok := val.DateTimeOK(); ok {
			return time.UnixMilli(v)
		}
	case bson.TypeRegex:
		if pattern, _, ok := val.RegexOK(); ok {
			return pattern
		}
	case bson.TypeJavaScript:
		if v, ok := val.JavaScriptOK(); ok {
			return primitive.JavaScript(v)
		}
	case bson.TypeSymbol:
		if v, ok := val.SymbolOK(); ok {
			return primitive.Symbol(v)
		}
	case bson.TypeDBPointer:
		if ns, oid, ok := val.DBPointerOK(); ok {
			return primitive.DBPointer{DB: ns, Pointer: oid}
		}
	case bson.TypeInt32:
		if v, ok := val.Int32OK(); ok {
			return v
		}
	case bson.TypeTimestamp:
		if t, _, ok := val.TimestampOK(); ok {
			return time.Unix(int64(t), 0)
		}
	case bson.TypeInt64:
		if v, ok := val.Int64OK(); ok {
			return v
		}
	case bson.TypeDecimal128:
		if v, ok := val.Decimal128OK(); ok {
			return v.String()
		}
	case bson.TypeUndefined:
		return primitive.Undefined{}
	case bson.TypeNull:
		return nil
	case bson.TypeMinKey:
		return primitive.MinKey{}
	case bson.TypeMaxKey:
		return primitive.MaxKey{}
	}
	var decoded any
	if err := val.Unmarshal(&decoded); err == nil {
		return decoded
	}
	return val.String()
}

func convertRawDocument(doc bson.Raw) map[string]any {
	elements, err := doc.Elements()
	if err != nil {
		return map[string]any{}
	}

	result := make(map[string]any, len(elements))
	for _, elem := range elements {
		result[elem.Key()] = convertRawBSONValue(elem.Value())
	}
	return result
}

func convertRawArray(arr bson.Raw) []any {
	values, err := arr.Values()
	if err != nil {
		return []any{}
	}

	result := make([]any, len(values))
	for i, value := range values {
		result[i] = convertRawBSONValue(value)
	}
	return result
}

func normalizeBatchSize(size int) int {
	if size <= 0 {
		return defaultBatchSize
	}

	return size
}

type mongoBatchBuilder struct {
	mem         memory.Allocator
	excludeMap  map[string]bool
	hasExcludes bool
	fieldOrder  []string
	cols        map[string]*typedColumnBuilder
	rowCount    int
}

type mongoRecordBatchBuilder interface {
	AppendDocument(doc bson.M) error
	AppendRawDocument(doc bson.Raw) error
	NewRecordBatch() (arrow.RecordBatch, error)
	Release()
}

func newMongoBatchBuilder(excludeColumns []string) *mongoBatchBuilder {
	excludeMap := make(map[string]bool, len(excludeColumns))
	for _, col := range excludeColumns {
		excludeMap[strings.ToLower(col)] = true
	}

	return &mongoBatchBuilder{
		mem:         memory.NewGoAllocator(),
		excludeMap:  excludeMap,
		hasExcludes: len(excludeMap) > 0,
		fieldOrder:  make([]string, 0),
		cols:        make(map[string]*typedColumnBuilder),
	}
}

func (b *mongoBatchBuilder) AppendDocument(doc bson.M) error {
	seen := make(map[string]bool, len(doc))
	for key, value := range doc {
		if b.isExcludedKey(key) {
			continue
		}
		col, ok := b.cols[key]
		if !ok {
			col = newTypedColumnBuilder(b.mem)
			col.AppendNulls(b.rowCount)
			b.cols[key] = col
			b.fieldOrder = append(b.fieldOrder, key)
		}
		col.Append(value)
		seen[key] = true
	}

	for _, field := range b.fieldOrder {
		if seen[field] {
			continue
		}
		b.cols[field].AppendNull()
	}

	b.rowCount++
	return nil
}

func (b *mongoBatchBuilder) AppendRawDocument(doc bson.Raw) error {
	var decoded bson.M
	if err := bson.Unmarshal(doc, &decoded); err != nil {
		return err
	}
	return b.AppendDocument(decoded)
}

func (b *mongoBatchBuilder) NewRecordBatch() (arrow.RecordBatch, error) {
	if len(b.fieldOrder) == 0 {
		emptySchema := arrow.NewSchema([]arrow.Field{}, nil)
		return array.NewRecordBatch(emptySchema, []arrow.Array{}, 0), nil
	}

	fieldOrder := append([]string(nil), b.fieldOrder...)
	sort.Strings(fieldOrder)

	fields := make([]arrow.Field, len(fieldOrder))
	arrays := make([]arrow.Array, len(fieldOrder))
	for i, name := range fieldOrder {
		arr, field := b.cols[name].Build(b.rowCount)
		field.Name = name
		fields[i] = field
		arrays[i] = arr
	}

	record := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(b.rowCount))

	for _, arr := range arrays {
		arr.Release()
	}
	b.reset()

	return record, nil
}

func (b *mongoBatchBuilder) Release() {
	for _, col := range b.cols {
		col.Release()
	}
	b.reset()
}

func (b *mongoBatchBuilder) reset() {
	b.fieldOrder = b.fieldOrder[:0]
	b.cols = make(map[string]*typedColumnBuilder)
	b.rowCount = 0
}

type mongoRawBatchBuilder struct {
	mem         memory.Allocator
	excludeMap  map[string]bool
	hasExcludes bool
	fieldOrder  []string
	columnIndex map[string]int
	columns     []*typedColumnBuilder
	seenAt      []uint64
	fieldAt     []int
	generation  uint64
	rowCapacity int
	rowCount    int
}

type rawDocumentField struct {
	index int
	value bson.RawValue
}

// transientString avoids allocating for map lookups. Callers must not retain it
// beyond the lifetime of data.
func transientString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}

func newMongoRawBatchBuilder(excludeColumns []string) *mongoRawBatchBuilder {
	return newMongoRawBatchBuilderWithCapacity(excludeColumns, 0)
}

func newMongoRawBatchBuilderWithCapacity(excludeColumns []string, rowCapacity int) *mongoRawBatchBuilder {
	return newMongoRawBatchBuilderWithAllocator(memory.NewGoAllocator(), excludeColumns, rowCapacity)
}

func newMongoRawBatchBuilderWithAllocator(mem memory.Allocator, excludeColumns []string, rowCapacity int) *mongoRawBatchBuilder {
	excludeMap := make(map[string]bool, len(excludeColumns))
	for _, col := range excludeColumns {
		excludeMap[strings.ToLower(col)] = true
	}

	return &mongoRawBatchBuilder{
		mem:         mem,
		excludeMap:  excludeMap,
		hasExcludes: len(excludeMap) > 0,
		fieldOrder:  make([]string, 0),
		columnIndex: make(map[string]int),
		rowCapacity: rowCapacity,
	}
}

func (b *mongoRawBatchBuilder) AppendDocument(doc bson.M) error {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return err
	}
	return b.AppendRawDocument(raw)
}

func (b *mongoRawBatchBuilder) AppendRawDocument(doc bson.Raw) error {
	length, rem, ok := bsoncore.ReadLength(doc)
	if !ok {
		return bsoncore.NewInsufficientBytesError(doc, rem)
	}
	length -= 4

	var stackFields [64]rawDocumentField
	fields := stackFields[:0]
	generation := b.nextGeneration()
	nextFieldIndex := 0

	for length > 1 {
		elem, next, ok := bsoncore.ReadElement(rem)
		if !ok {
			return bsoncore.NewInsufficientBytesError(doc, rem)
		}
		length -= int32(len(elem))
		rem = next

		keyBytes, err := elem.KeyBytesErr()
		if err != nil {
			return err
		}
		if b.isExcludedRawKey(keyBytes) {
			continue
		}
		val, err := elem.ValueErr()
		if err != nil {
			return err
		}

		key := transientString(keyBytes)
		index := nextFieldIndex
		exists := index < len(b.fieldOrder) && b.fieldOrder[index] == key
		if !exists {
			index, exists = b.columnIndex[key]
		}
		if !exists {
			key = string(keyBytes)
			index = len(b.columns)
			b.columnIndex[key] = index
			b.fieldOrder = append(b.fieldOrder, key)
			b.columns = append(b.columns, newTypedColumnBuilderWithCapacity(b.mem, b.rowCapacity))
			b.seenAt = append(b.seenAt, 0)
			b.fieldAt = append(b.fieldAt, 0)
		}
		nextFieldIndex = index + 1

		value := rawValueFromCore(val)
		if b.seenAt[index] == generation {
			fields[b.fieldAt[index]].value = value
			continue
		}
		b.seenAt[index] = generation
		b.fieldAt[index] = len(fields)
		fields = append(fields, rawDocumentField{
			index: index,
			value: value,
		})
	}

	for _, field := range fields {
		col := b.columns[field.index]
		if missing := b.rowCount - col.rowCount; missing > 0 {
			col.AppendNulls(missing)
		}
		col.AppendRaw(field.value)
	}

	b.rowCount++
	return nil
}

func (b *mongoRawBatchBuilder) nextGeneration() uint64 {
	b.generation++
	if b.generation != 0 {
		return b.generation
	}
	clear(b.seenAt)
	b.generation = 1
	return b.generation
}

func (b *mongoBatchBuilder) isExcludedKey(key string) bool {
	return b.hasExcludes && b.excludeMap[strings.ToLower(key)]
}

func (b *mongoRawBatchBuilder) isExcludedRawKey(key []byte) bool {
	return b.hasExcludes && b.excludeMap[strings.ToLower(string(key))]
}

func rawValueFromCore(val bsoncore.Value) bson.RawValue {
	return bson.RawValue{Type: val.Type, Value: val.Data}
}

func (b *mongoRawBatchBuilder) NewRecordBatch() (arrow.RecordBatch, error) {
	if len(b.fieldOrder) == 0 {
		emptySchema := arrow.NewSchema([]arrow.Field{}, nil)
		return array.NewRecordBatch(emptySchema, []arrow.Array{}, 0), nil
	}

	columnOrder := make([]int, len(b.fieldOrder))
	for i := range columnOrder {
		columnOrder[i] = i
	}
	sort.Slice(columnOrder, func(i, j int) bool {
		return b.fieldOrder[columnOrder[i]] < b.fieldOrder[columnOrder[j]]
	})

	fields := make([]arrow.Field, len(columnOrder))
	arrays := make([]arrow.Array, len(columnOrder))
	for i, columnIndex := range columnOrder {
		arr, field := b.columns[columnIndex].Build(b.rowCount)
		field.Name = b.fieldOrder[columnIndex]
		fields[i] = field
		arrays[i] = arr
	}

	record := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(b.rowCount))

	for _, arr := range arrays {
		arr.Release()
	}
	b.reset()

	return record, nil
}

func (b *mongoRawBatchBuilder) Release() {
	for _, col := range b.columns {
		col.Release()
	}
	b.reset()
}

func (b *mongoRawBatchBuilder) reset() {
	b.fieldOrder = b.fieldOrder[:0]
	b.columnIndex = make(map[string]int)
	b.columns = b.columns[:0]
	b.seenAt = b.seenAt[:0]
	b.fieldAt = b.fieldAt[:0]
	b.generation = 0
	b.rowCount = 0
}

type mongoSchemaBatchBuilder struct {
	columns     []schema.Column
	builders    []array.Builder
	columnIndex map[string]int
	columnRows  []int
	jsonBuffers []bytes.Buffer
	seenAt      []uint64
	fieldAt     []int
	generation  uint64
	rowCapacity int
	rowCount    int
}

func newMongoSchemaBatchBuilder(columns []schema.Column, excludeColumns []string) *mongoSchemaBatchBuilder {
	return newMongoSchemaBatchBuilderWithCapacity(columns, excludeColumns, 0)
}

func newMongoSchemaBatchBuilderWithCapacity(columns []schema.Column, excludeColumns []string, rowCapacity int) *mongoSchemaBatchBuilder {
	return newMongoSchemaBatchBuilderWithAllocator(memory.NewGoAllocator(), columns, excludeColumns, rowCapacity)
}

func newMongoSchemaBatchBuilderWithAllocator(mem memory.Allocator, columns []schema.Column, excludeColumns []string, rowCapacity int) *mongoSchemaBatchBuilder {
	excludeMap := make(map[string]bool, len(excludeColumns))
	for _, col := range excludeColumns {
		excludeMap[strings.ToLower(col)] = true
	}

	filtered := make([]schema.Column, 0, len(columns))
	for _, col := range columns {
		if excludeMap[strings.ToLower(col.Name)] {
			continue
		}
		filtered = append(filtered, col)
	}

	builders := make([]array.Builder, len(filtered))
	columnIndex := make(map[string]int, len(filtered))
	for i, col := range filtered {
		builders[i] = array.NewBuilder(mem, schema.DataTypeToArrowType(col))
		columnIndex[col.Name] = i
	}

	return &mongoSchemaBatchBuilder{
		columns:     filtered,
		builders:    builders,
		columnIndex: columnIndex,
		columnRows:  make([]int, len(filtered)),
		jsonBuffers: make([]bytes.Buffer, len(filtered)),
		seenAt:      make([]uint64, len(filtered)),
		fieldAt:     make([]int, len(filtered)),
		rowCapacity: rowCapacity,
	}
}

func (b *mongoSchemaBatchBuilder) AppendDocument(doc bson.M) error {
	b.reserveRows()
	for i, col := range b.columns {
		if missing := b.rowCount - b.columnRows[i]; missing > 0 {
			b.builders[i].AppendNulls(missing)
			b.columnRows[i] += missing
		}
		val, ok := doc[col.Name]
		if !ok || val == nil {
			b.builders[i].AppendNull()
			b.columnRows[i]++
			continue
		}
		arrowconv.AppendValue(b.builders[i], convertBSONValue(val))
		b.columnRows[i]++
	}
	b.rowCount++
	return nil
}

func (b *mongoSchemaBatchBuilder) AppendRawDocument(doc bson.Raw) error {
	b.reserveRows()
	length, rem, ok := bsoncore.ReadLength(doc)
	if !ok {
		return bsoncore.NewInsufficientBytesError(doc, rem)
	}
	length -= 4

	var stackFields [64]rawDocumentField
	fields := stackFields[:0]
	generation := b.nextGeneration()
	nextFieldIndex := 0
	for length > 1 {
		elem, next, ok := bsoncore.ReadElement(rem)
		if !ok {
			return bsoncore.NewInsufficientBytesError(doc, rem)
		}
		length -= int32(len(elem))
		rem = next

		key, err := elem.KeyBytesErr()
		if err != nil {
			return err
		}
		keyString := transientString(key)
		index := nextFieldIndex
		exists := index < len(b.columns) && b.columns[index].Name == keyString
		if !exists {
			index, exists = b.columnIndex[keyString]
		}
		if !exists {
			continue
		}
		nextFieldIndex = index + 1
		value, err := elem.ValueErr()
		if err != nil {
			return err
		}
		rawValue := rawValueFromCore(value)
		if b.seenAt[index] == generation {
			fields[b.fieldAt[index]].value = rawValue
			continue
		}
		b.seenAt[index] = generation
		b.fieldAt[index] = len(fields)
		fields = append(fields, rawDocumentField{index: index, value: rawValue})
	}

	for _, field := range fields {
		if missing := b.rowCount - b.columnRows[field.index]; missing > 0 {
			b.builders[field.index].AppendNulls(missing)
			b.columnRows[field.index] += missing
		}
		if field.value.Type == bson.TypeNull {
			b.builders[field.index].AppendNull()
		} else if !appendRawValue(b.builders[field.index], field.value, &b.jsonBuffers[field.index]) {
			arrowconv.AppendValue(b.builders[field.index], convertRawBSONValue(field.value))
		}
		b.columnRows[field.index]++
	}
	b.rowCount++
	return nil
}

func (b *mongoSchemaBatchBuilder) reserveRows() {
	if b.rowCount != 0 || b.rowCapacity <= 0 {
		return
	}
	for _, builder := range b.builders {
		builder.Reserve(b.rowCapacity)
	}
}

func (b *mongoSchemaBatchBuilder) nextGeneration() uint64 {
	b.generation++
	if b.generation != 0 {
		return b.generation
	}
	clear(b.seenAt)
	b.generation = 1
	return b.generation
}

func (b *mongoSchemaBatchBuilder) NewRecordBatch() (arrow.RecordBatch, error) {
	if len(b.columns) == 0 {
		emptySchema := arrow.NewSchema([]arrow.Field{}, nil)
		return array.NewRecordBatch(emptySchema, []arrow.Array{}, int64(b.rowCount)), nil
	}

	fields := make([]arrow.Field, len(b.columns))
	arrays := make([]arrow.Array, len(b.columns))
	for i, col := range b.columns {
		if missing := b.rowCount - b.columnRows[i]; missing > 0 {
			b.builders[i].AppendNulls(missing)
			b.columnRows[i] += missing
		}
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     schema.DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
		arrays[i] = b.builders[i].NewArray()
	}

	record := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(b.rowCount))

	for _, arr := range arrays {
		arr.Release()
	}
	b.Release()

	return record, nil
}

func (b *mongoSchemaBatchBuilder) Release() {
	for _, builder := range b.builders {
		if builder != nil {
			builder.Release()
		}
	}
	b.builders = nil
	b.columns = nil
	b.columnIndex = nil
	b.columnRows = nil
	b.jsonBuffers = nil
	b.seenAt = nil
	b.fieldAt = nil
	b.generation = 0
	b.rowCapacity = 0
	b.rowCount = 0
}

var _ source.Source = (*MongoDBSource)(nil)
