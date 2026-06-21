package mongodb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
		TableName:           tableName,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
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

		filter := bson.D{}
		if opts.IncrementalKey != "" {
			hasStart := opts.IntervalStart != nil
			hasEnd := opts.IntervalEnd != nil

			if hasStart || hasEnd {
				rangeFilter := bson.D{}
				if hasStart {
					rangeFilter = append(rangeFilter, bson.E{Key: "$gte", Value: opts.IntervalStart})
				}
				if hasEnd {
					rangeFilter = append(rangeFilter, bson.E{Key: "$lt", Value: opts.IntervalEnd})
				}
				filter = append(filter, bson.E{Key: opts.IncrementalKey, Value: rangeFilter})
			}

			findOpts.SetSort(bson.D{{Key: opts.IncrementalKey, Value: 1}})
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

	for {
		select {
		case <-ctx.Done():
			results <- source.RecordBatchResult{Err: ctx.Err()}
			return
		default:
		}

		startBatch := time.Now()
		var builder mongoRecordBatchBuilder = newMongoRawBatchBuilder(opts.ExcludeColumns)
		if opts.Schema != nil {
			builder = newMongoSchemaBatchBuilder(opts.Schema.Columns, opts.ExcludeColumns)
		}
		batchRows := 0

		for batchRows < batchSize && cursor.Next(ctx) {
			if opts.Schema == nil {
				if err := builder.AppendRawDocument(cursor.Current); err != nil {
					builder.Release()
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to build Arrow batch: %w", err)}
					return
				}
			} else {
				var doc bson.M
				if err := cursor.Decode(&doc); err != nil {
					builder.Release()
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to decode document: %w", err)}
					return
				}
				if err := builder.AppendDocument(doc); err != nil {
					builder.Release()
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to build Arrow batch: %w", err)}
					return
				}
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
	mem        memory.Allocator
	excludeMap map[string]bool
	fieldOrder []string
	cols       map[string]*typedColumnBuilder
	rowCount   int
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
		mem:        memory.NewGoAllocator(),
		excludeMap: excludeMap,
		fieldOrder: make([]string, 0),
		cols:       make(map[string]*typedColumnBuilder),
	}
}

func (b *mongoBatchBuilder) AppendDocument(doc bson.M) error {
	seen := make(map[string]bool, len(doc))
	for key, value := range doc {
		if b.excludeMap[strings.ToLower(key)] {
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
	mem        memory.Allocator
	excludeMap map[string]bool
	fieldOrder []string
	cols       map[string]*typedColumnBuilder
	rowCount   int
}

func newMongoRawBatchBuilder(excludeColumns []string) *mongoRawBatchBuilder {
	excludeMap := make(map[string]bool, len(excludeColumns))
	for _, col := range excludeColumns {
		excludeMap[strings.ToLower(col)] = true
	}

	return &mongoRawBatchBuilder{
		mem:        memory.NewGoAllocator(),
		excludeMap: excludeMap,
		fieldOrder: make([]string, 0),
		cols:       make(map[string]*typedColumnBuilder),
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
	elements, err := doc.Elements()
	if err != nil {
		return err
	}

	values := make(map[string]bson.RawValue, len(elements))
	keys := make([]string, 0, len(elements))
	for _, elem := range elements {
		key := elem.Key()
		if b.excludeMap[strings.ToLower(key)] {
			continue
		}
		if _, ok := values[key]; !ok {
			keys = append(keys, key)
		}
		values[key] = elem.Value()
	}

	for _, key := range keys {
		col, ok := b.cols[key]
		if !ok {
			col = newTypedColumnBuilder(b.mem)
			col.AppendNulls(b.rowCount)
			b.cols[key] = col
			b.fieldOrder = append(b.fieldOrder, key)
		}
		col.AppendRaw(values[key])
	}

	for _, field := range b.fieldOrder {
		if _, ok := values[field]; ok {
			continue
		}
		b.cols[field].AppendNull()
	}

	b.rowCount++
	return nil
}

func (b *mongoRawBatchBuilder) NewRecordBatch() (arrow.RecordBatch, error) {
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

func (b *mongoRawBatchBuilder) Release() {
	for _, col := range b.cols {
		col.Release()
	}
	b.reset()
}

func (b *mongoRawBatchBuilder) reset() {
	b.fieldOrder = b.fieldOrder[:0]
	b.cols = make(map[string]*typedColumnBuilder)
	b.rowCount = 0
}

type mongoSchemaBatchBuilder struct {
	columns  []schema.Column
	builders []array.Builder
	rowCount int
}

func newMongoSchemaBatchBuilder(columns []schema.Column, excludeColumns []string) *mongoSchemaBatchBuilder {
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

	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(filtered))
	for i, col := range filtered {
		builders[i] = array.NewBuilder(mem, schema.DataTypeToArrowType(col))
	}

	return &mongoSchemaBatchBuilder{
		columns:  filtered,
		builders: builders,
	}
}

func (b *mongoSchemaBatchBuilder) AppendDocument(doc bson.M) error {
	for i, col := range b.columns {
		val, ok := doc[col.Name]
		if !ok || val == nil {
			b.builders[i].AppendNull()
			continue
		}
		arrowconv.AppendValue(b.builders[i], convertBSONValue(val))
	}
	b.rowCount++
	return nil
}

func (b *mongoSchemaBatchBuilder) AppendRawDocument(doc bson.Raw) error {
	var decoded bson.M
	if err := bson.Unmarshal(doc, &decoded); err != nil {
		return err
	}
	return b.AppendDocument(decoded)
}

func (b *mongoSchemaBatchBuilder) NewRecordBatch() (arrow.RecordBatch, error) {
	if len(b.columns) == 0 {
		emptySchema := arrow.NewSchema([]arrow.Field{}, nil)
		return array.NewRecordBatch(emptySchema, []arrow.Array{}, int64(b.rowCount)), nil
	}

	fields := make([]arrow.Field, len(b.columns))
	arrays := make([]arrow.Array, len(b.columns))
	for i, col := range b.columns {
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
	b.rowCount = 0
}

var _ source.Source = (*MongoDBSource)(nil)
