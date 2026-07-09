package mongodb

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDBCDCMode string

const (
	MongoDBCDCModeBatch  MongoDBCDCMode = "batch"
	MongoDBCDCModeStream MongoDBCDCMode = "stream"

	defaultMongoDBCDCAwaitTime        = time.Second
	defaultMongoDBCDCSchemaSampleSize = 1000
	defaultMongoDBCDCStreamBatchSize  = 10000
	defaultMongoDBCDCFlushInterval    = 30 * time.Second

	// mongoLagRefreshInterval throttles the operationTime round-trip taken on
	// the idle path to keep the reported lag from drifting upward.
	mongoLagRefreshInterval = 5 * time.Second
)

var mongodbCDCColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type MongoDBCDCConfig struct {
	Mode             MongoDBCDCMode
	DestSchema       string
	MaxAwaitTime     time.Duration
	SchemaSampleSize int
}

type MongoDBCDCSource struct {
	client    *mongo.Client
	database  string
	uri       string
	cdcConfig MongoDBCDCConfig
	lag       *mongoLagState
}

// mongoLagState tracks the cluster time of the last processed change event
// against the server's own operationTime. Both clocks are server-side, so the
// difference is immune to client clock skew. Written by the change-stream
// goroutine, read by the metrics scraper.
type mongoLagState struct {
	lastEventUnix atomic.Int64
	serverOpUnix  atomic.Int64
	streaming     atomic.Bool
}

func newMongoLagState() *mongoLagState {
	return &mongoLagState{}
}

func (l *mongoLagState) noteEvent(ts primitive.Timestamp) {
	storeMaxInt64(&l.lastEventUnix, int64(ts.T))
}

func (l *mongoLagState) noteServerTime(ts primitive.Timestamp) {
	storeMaxInt64(&l.serverOpUnix, int64(ts.T))
}

func storeMaxInt64(dst *atomic.Int64, v int64) {
	for {
		cur := dst.Load()
		if v <= cur || dst.CompareAndSwap(cur, v) {
			return
		}
	}
}

type mongoNamespace struct {
	Database   string
	Collection string
	Name       string
}

type MongoDBCDCTable struct {
	source      *MongoDBCDCSource
	ns          mongoNamespace
	tableSchema *schema.TableSchema
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

type mongoCDCStart struct {
	OperationTime primitive.Timestamp
	ResumeToken   bson.Raw
}

type mongoCDCChangeEvent struct {
	ID            bson.Raw            `bson:"_id"`
	OperationType string              `bson:"operationType"`
	ClusterTime   primitive.Timestamp `bson:"clusterTime"`
	DocumentKey   bson.M              `bson:"documentKey"`
	FullDocument  bson.M              `bson:"fullDocument"`
}

type mongoCDCEventBuffer struct {
	tableSchema     *schema.TableSchema
	excludeColumns  []string
	tableName       string
	batchSize       int
	builder         *mongoSchemaBatchBuilder
	rows            int
	allowedColumns  map[string]struct{}
	excludedColumns map[string]struct{}
	warnedUnknown   map[string]struct{}
}

func NewMongoDBCDCSource() *MongoDBCDCSource {
	return &MongoDBCDCSource{lag: newMongoLagState()}
}

var _ source.LagReporter = (*MongoDBCDCSource)(nil)

// ReplicationLag reports how many seconds of change events the destination
// still trails the source by. Both timestamps come from the server, so an idle
// collection converges to zero rather than growing as "time since last change".
func (s *MongoDBCDCSource) ReplicationLag() (source.LagSnapshot, bool) {
	if s.lag == nil || !s.lag.streaming.Load() {
		return source.LagSnapshot{}, false
	}
	server := s.lag.serverOpUnix.Load()
	lastEvent := s.lag.lastEventUnix.Load()
	if server == 0 || lastEvent == 0 {
		return source.LagSnapshot{}, false
	}

	behind := float64(0)
	if server > lastEvent {
		behind = float64(server - lastEvent)
	}

	return source.LagSnapshot{
		Source:          "mongodb",
		SecondsBehind:   &behind,
		ServerPosition:  strconv.FormatInt(server, 10),
		DurablePosition: strconv.FormatInt(lastEvent, 10),
		CaughtUp:        behind == 0,
		UpdatedAt:       time.Now(),
	}, true
}

func (s *MongoDBCDCSource) Schemes() []string {
	return []string{"mongodb+cdc", "mongodb+srv+cdc"}
}

func (s *MongoDBCDCSource) Connect(ctx context.Context, rawURI string) error {
	cdcConfig, normalizedURI, err := parseMongoDBCDCURI(rawURI)
	if err != nil {
		return fmt.Errorf("failed to parse MongoDB CDC URI: %w", err)
	}

	clientOpts := options.Client().ApplyURI(normalizedURI)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	s.client = client
	s.database = extractDatabase(normalizedURI)
	s.uri = rawURI
	s.cdcConfig = cdcConfig
	config.Debug("[MONGODB CDC] Connected to database: %s", s.database)
	return nil
}

func (s *MongoDBCDCSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Disconnect(ctx)
	}
	return nil
}

func (s *MongoDBCDCSource) HandlesIncrementality() bool {
	return true
}

func (s *MongoDBCDCSource) SupportsStreaming() bool {
	return true
}

func (s *MongoDBCDCSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *MongoDBCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("collection name is required")
	}

	ns, err := parseMongoCDCNamespace(s.database, req.Name)
	if err != nil {
		return nil, err
	}

	pks, err := mongoCDCPrimaryKeys(req.PrimaryKeys)
	if err != nil {
		return nil, err
	}

	tableSchema, err := s.inferCollectionSchema(ctx, ns, pks)
	if err != nil {
		return nil, err
	}

	strategy := config.StrategyMerge
	if req.Strategy != "" && req.Strategy != config.StrategyReplace {
		strategy = req.Strategy
	}

	return &MongoDBCDCTable{
		source:      s,
		ns:          ns,
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    strategy,
	}, nil
}

func (s *MongoDBCDCSource) IsMultiTable() bool {
	return true
}

func (s *MongoDBCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	return s.getTables(ctx, nil)
}

func (s *MongoDBCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	tables, err := s.getTables(ctx, opts.Tables)
	if err != nil {
		return nil, err
	}

	results := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(results)

		var wg sync.WaitGroup
		for _, table := range tables {
			table := table
			wg.Add(1)
			go func() {
				defer wg.Done()

				ns := mongoNamespace{
					Database:   s.database,
					Collection: table.Name,
					Name:       table.Name,
				}
				readOpts := opts.ReadOptions
				readOpts.CDCResumeLSN = opts.CDCResumeLSNs[table.Name]
				records, err := s.readNamespace(ctx, ns, table.Schema, readOpts, table.Name)
				if err != nil {
					sendMongoCDCResult(ctx, results, source.RecordBatchResult{Err: err, TableName: table.Name})
					return
				}
				for res := range records {
					if res.TableName == "" {
						res.TableName = table.Name
					}
					if !sendMongoCDCResult(ctx, results, res) {
						return
					}
				}
			}()
		}
		wg.Wait()
	}()

	return results, nil
}

func (t *MongoDBCDCTable) Name() string {
	return t.ns.Name
}

func (t *MongoDBCDCTable) PrimaryKeys() []string {
	return t.primaryKeys
}

func (t *MongoDBCDCTable) IncrementalKey() string {
	return ""
}

func (t *MongoDBCDCTable) Strategy() config.IncrementalStrategy {
	return t.strategy
}

func (t *MongoDBCDCTable) HasKnownSchema() bool {
	return true
}

func (t *MongoDBCDCTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *MongoDBCDCTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	return t.source.readNamespace(ctx, t.ns, t.tableSchema, opts, "")
}

func parseMongoDBCDCURI(rawURI string) (MongoDBCDCConfig, string, error) {
	cfg := MongoDBCDCConfig{
		Mode:             MongoDBCDCModeBatch,
		MaxAwaitTime:     defaultMongoDBCDCAwaitTime,
		SchemaSampleSize: defaultMongoDBCDCSchemaSampleSize,
	}

	parsed, err := url.Parse(rawURI)
	if err != nil {
		return cfg, "", err
	}

	baseScheme, ok := strings.CutSuffix(strings.ToLower(parsed.Scheme), "+cdc")
	if !ok {
		return cfg, "", fmt.Errorf("unsupported MongoDB CDC scheme: %s", parsed.Scheme)
	}
	switch baseScheme {
	case "mongodb", "mongodb+srv":
		parsed.Scheme = baseScheme
	default:
		return cfg, "", fmt.Errorf("unsupported MongoDB CDC scheme: %s", parsed.Scheme)
	}

	query := parsed.Query()
	cfg.DestSchema = query.Get("dest_schema")
	if mode := strings.ToLower(strings.TrimSpace(query.Get("mode"))); mode != "" {
		switch mode {
		case string(MongoDBCDCModeBatch):
			cfg.Mode = MongoDBCDCModeBatch
		case string(MongoDBCDCModeStream):
			cfg.Mode = MongoDBCDCModeStream
		default:
			return cfg, "", fmt.Errorf("invalid mode: %s (must be 'batch' or 'stream')", mode)
		}
	}
	if maxAwait := strings.TrimSpace(query.Get("max_await_time")); maxAwait != "" {
		d, err := time.ParseDuration(maxAwait)
		if err != nil {
			return cfg, "", fmt.Errorf("invalid max_await_time: %w", err)
		}
		if d <= 0 {
			return cfg, "", fmt.Errorf("max_await_time must be positive")
		}
		cfg.MaxAwaitTime = d
	}
	if sampleSize := strings.TrimSpace(query.Get("schema_sample_size")); sampleSize != "" {
		var parsedSize int
		if _, err := fmt.Sscanf(sampleSize, "%d", &parsedSize); err != nil || parsedSize < 0 {
			return cfg, "", fmt.Errorf("schema_sample_size must be a non-negative integer")
		}
		cfg.SchemaSampleSize = parsedSize
	}

	query.Del("dest_schema")
	query.Del("mode")
	query.Del("max_await_time")
	query.Del("schema_sample_size")
	parsed.RawQuery = query.Encode()

	return cfg, parsed.String(), nil
}

func parseMongoCDCNamespace(defaultDB, table string) (mongoNamespace, error) {
	if strings.Contains(table, ":") {
		return mongoNamespace{}, fmt.Errorf("MongoDB CDC does not support aggregation pipelines in source-table")
	}

	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return mongoNamespace{Database: parts[0], Collection: parts[1], Name: table}, nil
	}
	if defaultDB == "" {
		return mongoNamespace{}, fmt.Errorf("database not specified: provide it in the URI or use database.collection format in source_table")
	}
	if table == "" {
		return mongoNamespace{}, fmt.Errorf("collection name is empty")
	}
	return mongoNamespace{Database: defaultDB, Collection: table, Name: table}, nil
}

func mongoCDCPrimaryKeys(pks []string) ([]string, error) {
	if len(pks) == 0 {
		return []string{"_id"}, nil
	}
	if len(pks) == 1 && pks[0] == "_id" {
		return pks, nil
	}
	return nil, fmt.Errorf("MongoDB CDC currently requires _id as the primary key because delete events only include documentKey")
}

func (s *MongoDBCDCSource) getTables(ctx context.Context, filter []string) ([]source.SourceTableInfo, error) {
	if s.database == "" {
		return nil, fmt.Errorf("MongoDB CDC multi-table mode requires a database in the source URI")
	}

	collections, err := s.client.Database(s.database).ListCollectionNames(ctx, bson.D{{Key: "type", Value: "collection"}})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections in database %q: %w", s.database, err)
	}
	sort.Strings(collections)

	selected := make([]source.SourceTableInfo, 0, len(collections))
	pks := []string{"_id"}
	for _, collection := range collections {
		if len(filter) > 0 && !mongoCDCMatchesTable(filter, s.database, collection) {
			continue
		}
		ns := mongoNamespace{Database: s.database, Collection: collection, Name: collection}
		tableSchema, err := s.inferCollectionSchema(ctx, ns, pks)
		if err != nil {
			return nil, fmt.Errorf("failed to infer schema for %s: %w", collection, err)
		}
		selected = append(selected, source.SourceTableInfo{
			Name:        collection,
			Schema:      tableSchema,
			PrimaryKeys: pks,
			DestSchema:  s.cdcConfig.DestSchema,
		})
	}
	if len(selected) == 0 {
		if len(filter) > 0 {
			return nil, fmt.Errorf("no MongoDB collections matched %s", strings.Join(filter, ", "))
		}
		return nil, fmt.Errorf("no MongoDB collections found in database %s", s.database)
	}
	return selected, nil
}

func mongoCDCMatchesTable(filter []string, db, collection string) bool {
	for _, table := range filter {
		if table == collection || table == db+"."+collection {
			return true
		}
	}
	return false
}

func (s *MongoDBCDCSource) inferCollectionSchema(ctx context.Context, ns mongoNamespace, primaryKeys []string) (*schema.TableSchema, error) {
	collection := s.client.Database(ns.Database).Collection(ns.Collection)
	builder := newMongoBatchBuilder(nil)
	defer builder.Release()

	rows := 0
	if s.cdcConfig.SchemaSampleSize > 0 {
		findOpts := options.Find().SetLimit(int64(s.cdcConfig.SchemaSampleSize))
		cursor, err := collection.Find(ctx, bson.D{}, findOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to sample collection %s.%s: %w", ns.Database, ns.Collection, err)
		}
		defer func() { _ = cursor.Close(ctx) }()

		for cursor.Next(ctx) {
			var doc bson.M
			if err := cursor.Decode(&doc); err != nil {
				return nil, fmt.Errorf("failed to decode sample document: %w", err)
			}
			if err := builder.AppendDocument(doc); err != nil {
				return nil, fmt.Errorf("failed to build sample schema: %w", err)
			}
			rows++
		}
		if err := cursor.Err(); err != nil {
			return nil, fmt.Errorf("sample cursor error: %w", err)
		}
	}

	var columns []schema.Column
	if rows > 0 {
		record, err := builder.NewRecordBatch()
		if err != nil {
			return nil, fmt.Errorf("failed to materialize sample schema: %w", err)
		}
		for _, field := range record.Schema().Fields() {
			columns = append(columns, schemainfer.ArrowFieldToColumn(field.Name, field.Type, true))
		}
		record.Release()
	}

	columns = ensureMongoCDCPKColumns(columns, primaryKeys)
	tableSchema := &schema.TableSchema{
		Name:        ns.Collection,
		Schema:      ns.Database,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}
	markMongoCDCPrimaryKeys(tableSchema)
	return addMongoCDCColumns(tableSchema), nil
}

func ensureMongoCDCPKColumns(columns []schema.Column, primaryKeys []string) []schema.Column {
	seen := make(map[string]bool, len(columns))
	for _, col := range columns {
		seen[strings.ToLower(col.Name)] = true
	}
	for _, pk := range primaryKeys {
		if seen[strings.ToLower(pk)] {
			continue
		}
		columns = append(columns, schema.Column{
			Name:         pk,
			DataType:     schema.TypeString,
			Nullable:     false,
			IsPrimaryKey: true,
		})
	}
	return columns
}

func markMongoCDCPrimaryKeys(tableSchema *schema.TableSchema) {
	pkSet := make(map[string]bool, len(tableSchema.PrimaryKeys))
	for _, pk := range tableSchema.PrimaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}
	for i := range tableSchema.Columns {
		if pkSet[strings.ToLower(tableSchema.Columns[i].Name)] {
			tableSchema.Columns[i].IsPrimaryKey = true
			tableSchema.Columns[i].Nullable = false
		}
	}
}

func addMongoCDCColumns(tableSchema *schema.TableSchema) *schema.TableSchema {
	copied := *tableSchema
	copied.Columns = append(append([]schema.Column{}, tableSchema.Columns...), mongodbCDCColumns...)
	return &copied
}

func (s *MongoDBCDCSource) readNamespace(ctx context.Context, ns mongoNamespace, tableSchema *schema.TableSchema, opts source.ReadOptions, resultTable string) (<-chan source.RecordBatchResult, error) {
	outputSchema := tableSchema
	if opts.Schema != nil {
		outputSchema = opts.Schema
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		mode := s.cdcConfig.Mode
		if opts.Streaming {
			mode = MongoDBCDCModeStream
		}

		hasResume := strings.TrimSpace(opts.CDCResumeLSN) != "" && !opts.FullRefresh
		var commandStart primitive.Timestamp
		if mode == MongoDBCDCModeBatch || !hasResume {
			opTime, err := s.currentOperationTime(ctx)
			if err != nil {
				results <- source.RecordBatchResult{Err: err, TableName: resultTable}
				return
			}
			commandStart = opTime
		}

		var batchTarget *primitive.Timestamp
		if mode == MongoDBCDCModeBatch {
			target := commandStart
			batchTarget = &target
		}

		start := mongoCDCStart{}
		if hasResume {
			parsedStart, err := parseMongoCDCLSN(opts.CDCResumeLSN)
			if err != nil {
				results <- source.RecordBatchResult{Err: err, TableName: resultTable}
				return
			}
			start = parsedStart
		} else {
			start.OperationTime = commandStart
			snapshotLSN := formatMongoCDCLSN(commandStart, nil)
			if err := s.snapshotCollection(ctx, ns, outputSchema, opts, snapshotLSN, results, resultTable); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed for %s.%s: %w", ns.Database, ns.Collection, err), TableName: resultTable}
				return
			}
		}

		if err := s.streamCollection(ctx, ns, outputSchema, opts, mode, start, batchTarget, results, resultTable); err != nil {
			results <- source.RecordBatchResult{Err: err, TableName: resultTable}
		}
	}()

	return results, nil
}

func (s *MongoDBCDCSource) currentOperationTime(ctx context.Context) (primitive.Timestamp, error) {
	db := s.database
	if db == "" {
		db = "admin"
	}

	var result struct {
		OperationTime primitive.Timestamp `bson:"operationTime"`
	}
	if err := s.client.Database(db).RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&result); err != nil {
		return primitive.Timestamp{}, fmt.Errorf("failed to read MongoDB operation time: %w", err)
	}
	if result.OperationTime.T == 0 {
		return primitive.Timestamp{}, fmt.Errorf("MongoDB did not return operationTime; CDC requires a replica set or sharded cluster")
	}
	return result.OperationTime, nil
}

func (s *MongoDBCDCSource) snapshotCollection(ctx context.Context, ns mongoNamespace, tableSchema *schema.TableSchema, opts source.ReadOptions, snapshotLSN string, results chan<- source.RecordBatchResult, resultTable string) error {
	collection := s.client.Database(ns.Database).Collection(ns.Collection)
	findOpts := options.Find().SetBatchSize(int32(mongoCDCReadBatchSize(opts)))
	cursor, err := collection.Find(ctx, bson.D{}, findOpts)
	if err != nil {
		return fmt.Errorf("failed to query snapshot: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	buffer := newMongoCDCEventBuffer(tableSchema, opts.ExcludeColumns, resultTable, mongoCDCReadBatchSize(opts))
	defer buffer.release()

	syncedAt := time.Now().UTC()
	rowSeq := int64(0)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return fmt.Errorf("failed to decode snapshot document: %w", err)
		}
		addMongoCDCMetadata(doc, snapshotLSN, false, syncedAt.Add(time.Duration(rowSeq)*time.Microsecond))
		if err := buffer.append(ctx, doc, results); err != nil {
			return err
		}
		rowSeq++
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("snapshot cursor error: %w", err)
	}
	return buffer.flush(ctx, results)
}

func (s *MongoDBCDCSource) streamCollection(ctx context.Context, ns mongoNamespace, tableSchema *schema.TableSchema, opts source.ReadOptions, mode MongoDBCDCMode, start mongoCDCStart, batchTarget *primitive.Timestamp, results chan<- source.RecordBatchResult, resultTable string) error {
	collection := s.client.Database(ns.Database).Collection(ns.Collection)
	maxAwaitTime := s.cdcConfig.MaxAwaitTime
	if opts.Streaming {
		flushInterval := mongoCDCFlushInterval(opts)
		if flushInterval > 0 && flushInterval < maxAwaitTime {
			maxAwaitTime = flushInterval
		}
	}
	watchOpts := options.ChangeStream().
		SetFullDocument(options.UpdateLookup).
		SetMaxAwaitTime(maxAwaitTime).
		SetBatchSize(int32(mongoCDCSourceBatchSize(opts)))
	if len(start.ResumeToken) > 0 {
		watchOpts.SetResumeAfter(start.ResumeToken)
	} else if start.OperationTime.T > 0 {
		watchOpts.SetStartAtOperationTime(&start.OperationTime)
	}

	stream, err := collection.Watch(ctx, mongo.Pipeline{}, watchOpts)
	if err != nil {
		return fmt.Errorf("failed to open MongoDB change stream for %s.%s: %w", ns.Database, ns.Collection, err)
	}
	defer func() { _ = stream.Close(ctx) }()

	buffer := newMongoCDCEventBuffer(tableSchema, opts.ExcludeColumns, resultTable, mongoCDCSourceBatchSize(opts))
	defer buffer.release()

	s.lag.streaming.Store(opts.Streaming)
	if start.OperationTime.T > 0 {
		s.lag.noteEvent(start.OperationTime)
		s.lag.noteServerTime(start.OperationTime)
	}
	// Refreshing the server clock costs a command round-trip, so only do it on
	// the idle path and no more than once per interval.
	var lastServerTimeRefresh time.Time
	refreshServerTime := func() {
		if !opts.Streaming || time.Since(lastServerTimeRefresh) < mongoLagRefreshInterval {
			return
		}
		lastServerTimeRefresh = time.Now()
		if ts, err := s.currentOperationTime(ctx); err == nil {
			s.lag.noteServerTime(ts)
		}
	}

	var firstBufferedAt time.Time
	flushByInterval := func() error {
		if !opts.Streaming || buffer.rows == 0 || firstBufferedAt.IsZero() || time.Since(firstBufferedAt) < mongoCDCFlushInterval(opts) {
			return nil
		}
		if err := buffer.flush(ctx, results); err != nil {
			return err
		}
		firstBufferedAt = time.Time{}
		return nil
	}

	for {
		if stream.TryNext(ctx) {
			var event mongoCDCChangeEvent
			if err := stream.Decode(&event); err != nil {
				return fmt.Errorf("failed to decode MongoDB change event: %w", err)
			}

			clusterTime := event.ClusterTime
			if clusterTime.T == 0 {
				clusterTime = primitive.Timestamp{T: uint32(time.Now().Unix())}
			}
			if mongoCDCAfterBatchTarget(clusterTime, batchTarget) {
				return buffer.flush(ctx, results)
			}
			// Only noteEvent here: advancing the server clock to the event's
			// own cluster time would make lag read zero while a backlog drains.
			s.lag.noteEvent(clusterTime)
			refreshServerTime()

			doc, deleted, ok := mongoCDCEventDocument(event)
			if !ok {
				continue
			}
			token := stream.ResumeToken()
			if len(token) == 0 {
				token = event.ID
			}
			addMongoCDCMetadata(doc, formatMongoCDCLSN(clusterTime, token), deleted, time.Now().UTC())
			wasEmpty := buffer.rows == 0
			if err := buffer.append(ctx, doc, results); err != nil {
				return err
			}
			if opts.Streaming {
				if wasEmpty && buffer.rows > 0 {
					firstBufferedAt = time.Now()
				}
				if buffer.rows == 0 {
					firstBufferedAt = time.Time{}
				}
				if err := flushByInterval(); err != nil {
					return err
				}
			}
			continue
		}

		if err := stream.Err(); err != nil {
			if ctx.Err() != nil {
				if opts.Streaming {
					if flushErr := buffer.flushBlocking(ctx, results); flushErr != nil {
						return flushErr
					}
				}
				return ctx.Err()
			}
			return fmt.Errorf("MongoDB change stream error for %s.%s: %w", ns.Database, ns.Collection, err)
		}

		if mode == MongoDBCDCModeBatch {
			return buffer.flush(ctx, results)
		}

		refreshServerTime()

		if err := flushByInterval(); err != nil {
			return err
		}

		if ctx.Err() != nil {
			if opts.Streaming {
				if flushErr := buffer.flushBlocking(ctx, results); flushErr != nil {
					return flushErr
				}
			}
			return ctx.Err()
		}
	}
}

func mongoCDCEventDocument(event mongoCDCChangeEvent) (bson.M, bool, bool) {
	switch event.OperationType {
	case "insert":
		doc := cloneBSONM(event.FullDocument)
		if len(doc) == 0 {
			doc = cloneBSONM(event.DocumentKey)
		}
		for key, value := range event.DocumentKey {
			if _, ok := doc[key]; !ok {
				doc[key] = value
			}
		}
		return doc, false, true
	case "replace", "update":
		doc := cloneBSONM(event.FullDocument)
		if len(doc) == 0 {
			return nil, false, false
		}
		for key, value := range event.DocumentKey {
			if _, ok := doc[key]; !ok {
				doc[key] = value
			}
		}
		return doc, false, true
	case "delete":
		return cloneBSONM(event.DocumentKey), true, true
	default:
		return nil, false, false
	}
}

func cloneBSONM(in bson.M) bson.M {
	out := make(bson.M, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func addMongoCDCMetadata(doc bson.M, lsn string, deleted bool, syncedAt time.Time) {
	doc[destination.CDCLSNColumn] = lsn
	doc[destination.CDCDeletedColumn] = deleted
	doc[destination.CDCSyncedAtColumn] = syncedAt.UTC()
}

func formatMongoCDCLSN(ts primitive.Timestamp, token bson.Raw) string {
	tokenHex := "0"
	if len(token) > 0 {
		tokenHex = hex.EncodeToString(token)
	}
	return fmt.Sprintf("%010d:%010d:%s", ts.T, ts.I, tokenHex)
}

func parseMongoCDCLSN(stored string) (mongoCDCStart, error) {
	parts := strings.SplitN(strings.TrimSpace(stored), ":", 3)
	if len(parts) < 2 {
		return mongoCDCStart{}, fmt.Errorf("MongoDB CDC resume LSN %q is invalid; run with --full-refresh to rebuild the destination safely", stored)
	}

	var t, i uint32
	if _, err := fmt.Sscanf(parts[0], "%d", &t); err != nil {
		return mongoCDCStart{}, fmt.Errorf("MongoDB CDC resume LSN %q has invalid timestamp: %w", stored, err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &i); err != nil {
		return mongoCDCStart{}, fmt.Errorf("MongoDB CDC resume LSN %q has invalid increment: %w", stored, err)
	}

	start := mongoCDCStart{OperationTime: primitive.Timestamp{T: t, I: i}}
	if len(parts) == 3 && parts[2] != "" && parts[2] != "0" {
		token, err := hex.DecodeString(parts[2])
		if err != nil {
			return mongoCDCStart{}, fmt.Errorf("MongoDB CDC resume LSN %q has invalid resume token: %w", stored, err)
		}
		start.ResumeToken = bson.Raw(token)
	}
	return start, nil
}

func mongoCDCAfterBatchTarget(ts primitive.Timestamp, target *primitive.Timestamp) bool {
	if target == nil {
		return false
	}
	return compareMongoCDCTimestamp(ts, *target) > 0
}

func compareMongoCDCTimestamp(a, b primitive.Timestamp) int {
	if a.T < b.T {
		return -1
	}
	if a.T > b.T {
		return 1
	}
	if a.I < b.I {
		return -1
	}
	if a.I > b.I {
		return 1
	}
	return 0
}

func newMongoCDCEventBuffer(tableSchema *schema.TableSchema, excludeColumns []string, tableName string, batchSize int) *mongoCDCEventBuffer {
	if batchSize <= 0 {
		batchSize = defaultMongoDBCDCStreamBatchSize
	}
	allowedColumns := make(map[string]struct{}, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		allowedColumns[col.Name] = struct{}{}
	}
	excludedColumns := make(map[string]struct{}, len(excludeColumns))
	for _, col := range excludeColumns {
		excludedColumns[strings.ToLower(col)] = struct{}{}
	}
	return &mongoCDCEventBuffer{
		tableSchema:     tableSchema,
		excludeColumns:  excludeColumns,
		tableName:       tableName,
		batchSize:       batchSize,
		builder:         newMongoSchemaBatchBuilder(tableSchema.Columns, excludeColumns),
		allowedColumns:  allowedColumns,
		excludedColumns: excludedColumns,
		warnedUnknown:   make(map[string]struct{}),
	}
}

func (b *mongoCDCEventBuffer) append(ctx context.Context, doc bson.M, results chan<- source.RecordBatchResult) error {
	if unknown := b.unknownDocumentFields(doc); len(unknown) > 0 {
		b.debugUnknownDocumentFields(unknown)
	}
	if err := b.builder.AppendDocument(doc); err != nil {
		return fmt.Errorf("failed to build MongoDB CDC Arrow batch: %w", err)
	}
	b.rows++
	if b.rows < b.batchSize {
		return nil
	}
	return b.flush(ctx, results)
}

func (b *mongoCDCEventBuffer) unknownDocumentFields(doc bson.M) []string {
	var unknown []string
	for key := range doc {
		if _, ok := b.allowedColumns[key]; ok {
			continue
		}
		if _, ok := b.excludedColumns[strings.ToLower(key)]; ok {
			continue
		}
		unknown = append(unknown, key)
	}
	sort.Strings(unknown)
	return unknown
}

func (b *mongoCDCEventBuffer) debugUnknownDocumentFields(fields []string) {
	newFields := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, ok := b.warnedUnknown[field]; ok {
			continue
		}
		b.warnedUnknown[field] = struct{}{}
		newFields = append(newFields, field)
	}
	if len(newFields) == 0 {
		return
	}
	tableName := b.tableName
	if tableName == "" && b.tableSchema != nil {
		tableName = b.tableSchema.Name
	}
	config.Debug("[MONGODB CDC] Ignoring fields not present in inferred schema for %s: %s", tableName, strings.Join(newFields, ", "))
}

func (b *mongoCDCEventBuffer) flush(ctx context.Context, results chan<- source.RecordBatchResult) error {
	if b.rows == 0 {
		return nil
	}
	record, err := b.builder.NewRecordBatch()
	if err != nil {
		return fmt.Errorf("failed to convert MongoDB CDC batch to Arrow: %w", err)
	}
	if !sendMongoCDCResult(ctx, results, source.RecordBatchResult{Batch: record, TableName: b.tableName}) {
		record.Release()
		return ctx.Err()
	}
	b.builder = newMongoSchemaBatchBuilder(b.tableSchema.Columns, b.excludeColumns)
	b.rows = 0
	return nil
}

func (b *mongoCDCEventBuffer) flushBlocking(ctx context.Context, results chan<- source.RecordBatchResult) error {
	if b.rows == 0 {
		return nil
	}
	record, err := b.builder.NewRecordBatch()
	if err != nil {
		return fmt.Errorf("failed to convert MongoDB CDC batch to Arrow: %w", err)
	}
	if !sendMongoCDCResult(ctx, results, source.RecordBatchResult{Batch: record, TableName: b.tableName}) {
		record.Release()
		return ctx.Err()
	}
	b.builder = newMongoSchemaBatchBuilder(b.tableSchema.Columns, b.excludeColumns)
	b.rows = 0
	return nil
}

func (b *mongoCDCEventBuffer) release() {
	if b.builder != nil {
		b.builder.Release()
		b.builder = nil
	}
}

func mongoCDCSourceBatchSize(opts source.ReadOptions) int {
	if opts.Streaming && opts.FlushRecords > 0 {
		return opts.FlushRecords
	}
	return mongoCDCReadBatchSize(opts)
}

func mongoCDCFlushInterval(opts source.ReadOptions) time.Duration {
	if opts.Streaming && opts.FlushInterval > 0 {
		return opts.FlushInterval
	}
	return defaultMongoDBCDCFlushInterval
}

func mongoCDCReadBatchSize(opts source.ReadOptions) int {
	if opts.PageSize > 0 {
		return opts.PageSize
	}
	return defaultMongoDBCDCStreamBatchSize
}

func sendMongoCDCResult(ctx context.Context, results chan<- source.RecordBatchResult, result source.RecordBatchResult) bool {
	select {
	case results <- result:
		return true
	case <-ctx.Done():
		if result.Batch != nil {
			result.Batch.Release()
		}
		return false
	}
}

var (
	_ source.Source           = (*MongoDBCDCSource)(nil)
	_ source.StreamingSource  = (*MongoDBCDCSource)(nil)
	_ source.MultiTableSource = (*MongoDBCDCSource)(nil)
	_ source.SourceTable      = (*MongoDBCDCTable)(nil)
)
