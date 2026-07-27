package duckdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-adbc/go/adbc/drivermgr"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/gofrs/flock"
	"golang.org/x/sync/errgroup"
)

type DuckDBDestination struct {
	filePath string
	catalog  string
	mu       sync.Mutex

	memoryLeaseMu   sync.Mutex
	memoryLeaseHeld bool

	db   adbc.Database
	conn adbc.Connection

	// schemas captures the schema each prepared table was created with, keyed by the
	// fully-qualified opts.Table name. SwapTable's cross-schema branch reads this to
	// recreate the target with full constraints (NOT NULL, PK) instead of losing them
	// via plain CTAS. Per-key writes mean parallel PrepareTable calls in multi-table
	// runs don't clobber each other.
	schemas   map[string]*schema.TableSchema
	schemasMu sync.Mutex
}

type duckDBManagedCDCRunLease struct {
	release     func() error
	done        chan struct{}
	releaseOnce sync.Once
	releaseErr  error
}

func NewDuckDBDestination() *DuckDBDestination {
	return &DuckDBDestination{}
}

func (d *DuckDBDestination) AcquireManagedCDCRunLease(_ context.Context, connectorID string) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, fmt.Errorf("managed CDC connector ID is empty")
	}
	if strings.HasPrefix(d.filePath, "md:") {
		return nil, fmt.Errorf("MotherDuck does not support local managed CDC run leases")
	}
	if d.filePath == ":memory:" {
		d.memoryLeaseMu.Lock()
		defer d.memoryLeaseMu.Unlock()
		if d.memoryLeaseHeld {
			return nil, fmt.Errorf("another DuckDB CDC run already owns the destination")
		}
		d.memoryLeaseHeld = true
		return &duckDBManagedCDCRunLease{
			done: make(chan struct{}),
			release: func() error {
				d.memoryLeaseMu.Lock()
				defer d.memoryLeaseMu.Unlock()
				d.memoryLeaseHeld = false
				return nil
			},
		}, nil
	}
	lockPath := d.filePath + ".ingestr-cdc.lock"
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire DuckDB CDC run lease: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("another DuckDB CDC run already owns the destination")
	}
	return &duckDBManagedCDCRunLease{release: fileLock.Unlock, done: make(chan struct{})}, nil
}

func (l *duckDBManagedCDCRunLease) Done() <-chan struct{} { return l.done }

func (l *duckDBManagedCDCRunLease) Err() error { return nil }

func (l *duckDBManagedCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.releaseErr = l.release()
	})
	return l.releaseErr
}

func (d *DuckDBDestination) recordSchema(table string, sch *schema.TableSchema, pks []string) {
	if sch == nil {
		return
	}
	clone := *sch
	clone.PrimaryKeys = append([]string(nil), pks...)
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	if d.schemas == nil {
		d.schemas = map[string]*schema.TableSchema{}
	}
	d.schemas[table] = &clone
}

func (d *DuckDBDestination) lookupSchema(table string) *schema.TableSchema {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	return d.schemas[table]
}

func (d *DuckDBDestination) forgetSchema(table string) {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	delete(d.schemas, table)
}

func (d *DuckDBDestination) Schemes() []string {
	return []string{"duckdb", "motherduck", "md"}
}

func (d *DuckDBDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:    destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema: "main",
	}
}

func (d *DuckDBDestination) Connect(ctx context.Context, uri string) error {
	path, err := parseDuckDBPath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse DuckDB URI: %w", err)
	}

	d.filePath = path
	config.Debug("[DUCKDB] Destination path: %s", d.filePath)

	isMotherDuck := strings.HasPrefix(d.filePath, "md:")
	if !isMotherDuck && d.filePath != ":memory:" {
		dir := filepath.Dir(d.filePath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		}
	}

	dialect := srcduckdb.NewDialect()
	if err := dialect.EnsureDriver(ctx); err != nil {
		return fmt.Errorf("failed to ensure DuckDB ADBC driver: %w", err)
	}

	db, err := (drivermgr.Driver{}).NewDatabaseWithContext(ctx, map[string]string{
		"driver": "duckdb",
		"path":   d.filePath,
	})
	if err != nil {
		return fmt.Errorf("failed to create DuckDB ADBC database: %w", err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to open DuckDB ADBC connection: %w", err)
	}

	d.db = db
	d.conn = conn

	// Ensure changes are committed/visible across connections by default.
	if opt, ok := conn.(adbc.PostInitOptions); ok {
		_ = opt.SetOption(adbc.OptionKeyAutoCommit, adbc.OptionValueEnabled)
	}

	if limit := os.Getenv("INGESTR_DUCKDB_MEMORY_LIMIT"); limit != "" {
		if strings.ContainsAny(limit, "';\n") {
			config.Debug("[DUCKDB] Ignoring invalid INGESTR_DUCKDB_MEMORY_LIMIT=%q", limit)
		} else if err := d.exec(ctx, fmt.Sprintf("SET memory_limit='%s'", limit)); err != nil {
			config.Debug("[DUCKDB] Failed to set memory_limit=%s: %v", limit, err)
		}
	}

	// Simple sanity check
	if err := d.exec(ctx, "SELECT 1"); err != nil {
		_ = d.conn.Close()
		_ = d.db.Close()
		d.conn = nil
		d.db = nil
		return fmt.Errorf("failed to validate DuckDB ADBC connection: %w", err)
	}

	return nil
}

func (d *DuckDBDestination) Close(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var err error
	if d.conn != nil {
		err = errorsJoin(err, d.conn.Close())
		d.conn = nil
	}
	if d.db != nil {
		err = errorsJoin(err, d.db.Close())
		d.db = nil
	}
	return err
}

func (d *DuckDBDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureSchemaExistsLocked(ctx, duckTable(opts.Table)); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(opts.Table))
		if err := d.exec(ctx, dropSQL); err != nil {
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[DUCKDB] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(destination.QuoteTableName(opts.Table), opts.Schema.Columns, opts.PrimaryKeys)
	config.Debug("[DUCKDB] CREATE SQL: %s", createSQL)
	if err := d.exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	preparedSchema, err := d.getTableSchemaLocked(ctx, opts.Table)
	if err != nil {
		return fmt.Errorf("failed to inspect prepared table: %w", err)
	}
	if preparedSchema == nil {
		return fmt.Errorf("prepared table %s was not found", opts.Table)
	}
	if opts.RequirePrimaryKeyMatch && !duckDBPrimaryKeySetsEqual(opts.PrimaryKeys, preparedSchema.PrimaryKeys) {
		return fmt.Errorf("CDC merge target %s must have primary key %v; found %v", opts.Table, opts.PrimaryKeys, preparedSchema.PrimaryKeys)
	}
	d.recordSchema(opts.Table, opts.Schema, preparedSchema.PrimaryKeys)
	config.Debug("[DUCKDB] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

func duckDBPrimaryKeySetsEqual(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	remaining := make(map[string]int, len(expected))
	for _, key := range expected {
		remaining[duckDBIdentifierKey(key)]++
	}
	for _, key := range actual {
		normalized := duckDBIdentifierKey(key)
		if remaining[normalized] == 0 {
			return false
		}
		remaining[normalized]--
	}
	return true
}

func duckDBIdentifierKey(identifier string) string {
	bytes := []byte(identifier)
	for i, ch := range bytes {
		if ch >= 'A' && ch <= 'Z' {
			bytes[i] = ch + ('a' - 'A')
		}
	}
	return string(bytes)
}

func (d *DuckDBDestination) ensureSchemaExists(ctx context.Context, tn tablename.TableName) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ensureSchemaExistsLocked(ctx, tn)
}

// ensureSchemaExistsLocked assumes the mutex is already held. When the table
// carries a catalog (attached database), the schema is created within it.
func (d *DuckDBDestination) ensureSchemaExistsLocked(ctx context.Context, tn tablename.TableName) error {
	if tn.Schema == "" || tn.Schema == "main" {
		return nil
	}

	target := destination.QuoteIdentifier(tn.Schema)
	if tn.Catalog != "" {
		target = destination.QuoteIdentifier(tn.Catalog) + "." + target
	}
	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", target)
	if err := d.exec(ctx, createSchemaSQL); err != nil {
		return fmt.Errorf("failed to create schema %s: %w", tn.Schema, err)
	}
	config.Debug("[DUCKDB] Ensured schema exists: %s", tn.Schema)
	return nil
}

func (d *DuckDBDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaADBCIngest(ctx, records, opts)
}

func (d *DuckDBDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaADBCIngest(ctx, records, opts)
}

func (d *DuckDBDestination) writeViaADBCIngest(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	defer drainAndReleaseDuckDBRecords(records)

	config.Debug("[DUCKDB] Starting write to %s", opts.Table)
	startTotal := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	tn := duckTable(opts.Table)
	tableName := tn.Table
	ingestOpts := adbc.IngestStreamOptions{}
	if tn.Schema != "" {
		ingestOpts.DBSchema = tn.Schema
	}
	if tn.Catalog != "" {
		ingestOpts.Catalog = tn.Catalog
	}

	// Optional periodic CHECKPOINT to bound DuckDB's WAL/buffer pool growth
	// during large ingests. Off by default. Set INGESTR_DUCKDB_CHECKPOINT_ROWS=<n>
	// to checkpoint after every n rows (-1 = after every batch).
	var checkpointEvery int64
	if v := os.Getenv("INGESTR_DUCKDB_CHECKPOINT_ROWS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			checkpointEvery = n
		} else {
			config.Debug("[DUCKDB] Invalid INGESTR_DUCKDB_CHECKPOINT_ROWS=%q, checkpointing disabled: %v", v, err)
		}
	}
	if checkpointEvery == 0 {
		if conns := d.ingestConnections(opts); conns > 1 {
			return d.writeViaParallelADBCIngest(ctx, records, tableName, ingestOpts, startTotal, conns)
		}
		return d.writeViaSingleADBCIngest(ctx, records, tableName, ingestOpts, startTotal)
	}

	var totalRows int64
	var rowsSinceCheckpoint int64
	for res := range records {
		if res.Err != nil {
			if res.Batch != nil {
				res.Batch.Release()
			}
			return res.Err
		}
		if res.Batch == nil {
			continue
		}
		if res.Batch.NumRows() == 0 {
			res.Batch.Release()
			continue
		}

		// Use standard Arrow RecordReader for each batch
		reader, readerErr := array.NewRecordReader(res.Batch.Schema(), []arrow.RecordBatch{res.Batch})
		if readerErr != nil {
			res.Batch.Release()
			return fmt.Errorf("failed to create record reader: %w", readerErr)
		}

		_, ingestErr := adbc.IngestStream(ctx, d.conn, reader, tableName, adbc.OptionValueIngestModeAppend, ingestOpts)
		reader.Release()

		if ingestErr != nil {
			config.Debug("[DUCKDB] IngestStream error: %v", ingestErr)
			res.Batch.Release()
			return fmt.Errorf("failed to ingest batch: %w", ingestErr)
		}

		totalRows += res.Batch.NumRows()
		rowsSinceCheckpoint += res.Batch.NumRows()
		res.Batch.Release()

		shouldCheckpoint := checkpointEvery == -1 ||
			(checkpointEvery > 0 && rowsSinceCheckpoint >= checkpointEvery)
		if shouldCheckpoint {
			if err := d.exec(ctx, "CHECKPOINT"); err != nil {
				config.Debug("[DUCKDB] CHECKPOINT failed: %v", err)
			}
			rowsSinceCheckpoint = 0
		}
	}

	totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
	config.Debug("[DUCKDB] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
	return nil
}

func drainAndReleaseDuckDBRecords(records <-chan source.RecordBatchResult) {
	for {
		select {
		case result, ok := <-records:
			if !ok {
				return
			}
			if result.Batch != nil {
				result.Batch.Release()
			}
		default:
			return
		}
	}
}

// ingestConnections decides how many connections to spread an ingest over.
// The single-threaded Arrow-stream push through the driver is the bottleneck
// for wide tables, and DuckDB handles concurrent appends to the same table
// transactionally. Kept at 1 for MotherDuck (remote sessions) and for tables
// with primary keys, where concurrent index appends can raise transaction
// conflicts.
func (d *DuckDBDestination) ingestConnections(opts destination.WriteOptions) int {
	preparedSchema := d.lookupSchema(opts.Table)
	if strings.HasPrefix(d.filePath, "md:") || len(opts.PrimaryKeys) > 0 ||
		(preparedSchema != nil && len(preparedSchema.PrimaryKeys) > 0) {
		return 1
	}
	if v := os.Getenv("INGESTR_DUCKDB_INGEST_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
		config.Debug("[DUCKDB] Ignoring invalid INGESTR_DUCKDB_INGEST_CONNS=%q", v)
	}
	return min(4, runtime.GOMAXPROCS(0))
}

// writeViaParallelADBCIngest runs several IngestStream appenders against the
// same table, each on its own connection, all pulling batches from the shared
// channel. Row order across workers is not preserved, which append semantics
// do not require.
func (d *DuckDBDestination) writeViaParallelADBCIngest(ctx context.Context, records <-chan source.RecordBatchResult, tableName string, ingestOpts adbc.IngestStreamOptions, startTotal time.Time, conns int) error {
	g, gctx := errgroup.WithContext(ctx)
	var totalRows atomic.Int64

	for range conns {
		g.Go(func() error {
			first, err := nextNonEmptyRecord(gctx, records)
			if err != nil {
				return err
			}
			if first == nil {
				return nil
			}

			conn, err := d.db.Open(gctx)
			if err != nil {
				first.Release()
				return fmt.Errorf("failed to open ingest connection: %w", err)
			}
			defer func() { _ = conn.Close() }()

			reader := newChannelRecordReader(gctx, records, first)
			_, ingestErr := adbc.IngestStream(gctx, conn, reader, tableName, adbc.OptionValueIngestModeAppend, ingestOpts)
			totalRows.Add(reader.rowsWritten())
			readerErr := reader.Err()
			reader.Release()

			if ingestErr != nil {
				config.Debug("[DUCKDB] IngestStream error: %v", ingestErr)
				return fmt.Errorf("failed to ingest batch: %w", ingestErr)
			}
			return readerErr
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	rows := totalRows.Load()
	totalRate := float64(rows) / time.Since(startTotal).Seconds()
	config.Debug("[DUCKDB] Total: %d rows written in %v across %d connections (%.0f rows/sec)", rows, time.Since(startTotal), conns, totalRate)
	return nil
}

func (d *DuckDBDestination) writeViaSingleADBCIngest(ctx context.Context, records <-chan source.RecordBatchResult, tableName string, ingestOpts adbc.IngestStreamOptions, startTotal time.Time) error {
	first, err := nextNonEmptyRecord(ctx, records)
	if err != nil {
		return err
	}
	if first == nil {
		config.Debug("[DUCKDB] Total: 0 rows written in %v (0 rows/sec)", time.Since(startTotal))
		return nil
	}

	reader := newChannelRecordReader(ctx, records, first)
	_, ingestErr := adbc.IngestStream(ctx, d.conn, reader, tableName, adbc.OptionValueIngestModeAppend, ingestOpts)
	totalRows := reader.rowsWritten()
	readerErr := reader.Err()
	reader.Release()

	if ingestErr != nil {
		config.Debug("[DUCKDB] IngestStream error: %v", ingestErr)
		return fmt.Errorf("failed to ingest batch: %w", ingestErr)
	}
	if readerErr != nil {
		return readerErr
	}

	totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
	config.Debug("[DUCKDB] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
	return nil
}

func nextNonEmptyRecord(ctx context.Context, records <-chan source.RecordBatchResult) (arrow.RecordBatch, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res, ok := <-records:
			if !ok {
				return nil, nil
			}
			if res.Err != nil {
				if res.Batch != nil {
					res.Batch.Release()
				}
				return nil, res.Err
			}
			if res.Batch == nil {
				continue
			}
			if res.Batch.NumRows() == 0 {
				res.Batch.Release()
				continue
			}
			return res.Batch, nil
		}
	}
}

type channelRecordReader struct {
	refCount atomic.Int64
	ctx      context.Context
	records  <-chan source.RecordBatchResult
	schema   *arrow.Schema
	first    arrow.RecordBatch
	current  arrow.RecordBatch
	err      error
	rows     atomic.Int64
}

func newChannelRecordReader(ctx context.Context, records <-chan source.RecordBatchResult, first arrow.RecordBatch) *channelRecordReader {
	reader := &channelRecordReader{
		ctx:     ctx,
		records: records,
		schema:  first.Schema(),
		first:   first,
	}
	reader.refCount.Add(1)
	return reader
}

func (r *channelRecordReader) Retain() {
	r.refCount.Add(1)
}

func (r *channelRecordReader) Release() {
	if r.refCount.Add(-1) != 0 {
		return
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.first != nil {
		r.first.Release()
		r.first = nil
	}
}

func (r *channelRecordReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *channelRecordReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.err != nil {
		return false
	}
	if r.first != nil {
		r.current = r.first
		r.first = nil
		r.rows.Add(r.current.NumRows())
		return true
	}

	for {
		select {
		case <-r.ctx.Done():
			r.err = r.ctx.Err()
			return false
		case res, ok := <-r.records:
			if !ok {
				return false
			}
			if res.Err != nil {
				if res.Batch != nil {
					res.Batch.Release()
				}
				r.err = res.Err
				return false
			}
			if res.Batch == nil {
				continue
			}
			if res.Batch.NumRows() == 0 {
				res.Batch.Release()
				continue
			}
			batch := res.Batch
			if !batch.Schema().Equal(r.schema) {
				rewrapped, ok := rewrapRecordBatchWithSchema(batch, r.schema)
				if !ok {
					batch.Release()
					r.err = fmt.Errorf("record batch schema changed during DuckDB ingest")
					return false
				}
				res.Batch.Release()
				batch = rewrapped
			}
			r.current = batch
			r.rows.Add(r.current.NumRows())
			return true
		}
	}
}

func rewrapRecordBatchWithSchema(batch arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, bool) {
	if batch.Schema().NumFields() != target.NumFields() {
		return nil, false
	}

	cols := make([]arrow.Array, batch.NumCols())
	for i := 0; i < int(batch.NumCols()); i++ {
		sourceField := batch.Schema().Field(i)
		targetField := target.Field(i)
		if sourceField.Name != targetField.Name || !arrow.TypeEqual(sourceField.Type, targetField.Type) {
			return nil, false
		}
		cols[i] = batch.Column(i)
	}

	return array.NewRecordBatch(target, cols, batch.NumRows()), true
}

func (r *channelRecordReader) RecordBatch() arrow.RecordBatch {
	return r.current
}

func (r *channelRecordReader) Record() arrow.RecordBatch {
	return r.RecordBatch()
}

func (r *channelRecordReader) Err() error {
	return r.err
}

func (r *channelRecordReader) rowsWritten() int64 {
	return r.rows.Load()
}

var _ array.RecordReader = (*channelRecordReader)(nil)

func (d *DuckDBDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()
	if err := tablename.DuckDB.CheckName(opts.StagingTable); err != nil {
		return err
	}
	if err := tablename.DuckDB.CheckName(opts.TargetTable); err != nil {
		return err
	}

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	targetTn := duckTable(targetTable)
	stagingTn := duckTable(stagingTable)
	targetName := targetTn.Table

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()
	conditional := opts.CDCExpectedIncarnation != "" ||
		opts.CDCExpectedStagingIncarnation != "" ||
		opts.CDCExpectedResultIncarnation != ""
	if conditional {
		if opts.CDCExpectedIncarnation == "" || opts.CDCExpectedStagingIncarnation == "" || opts.CDCExpectedResultIncarnation == "" {
			return fmt.Errorf("DuckDB CDC conditional swap requires target, staging, and result incarnations")
		}
		if !duckSameNamespace(stagingTn, targetTn) {
			return fmt.Errorf("DuckDB CDC conditional swaps require staging and target tables in the same schema")
		}
		currentTarget, exists, err := d.cdcTargetIncarnationLocked(ctx, targetTable)
		if err != nil {
			return err
		}
		if !exists || currentTarget != opts.CDCExpectedIncarnation {
			return fmt.Errorf("DuckDB CDC target %s was replaced before conditional swap", targetTable)
		}
		currentStaging, exists, err := d.cdcTargetIncarnationLocked(ctx, stagingTable)
		if err != nil {
			return err
		}
		if !exists || currentStaging != opts.CDCExpectedStagingIncarnation {
			return fmt.Errorf("DuckDB CDC staging table %s was replaced before conditional swap", stagingTable)
		}
	}

	if duckSameNamespace(stagingTn, targetTn) {
		// Same catalog+schema: cheap rename swap.
		oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
		oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("duckdb"))
		// The renamed-away target keeps the target's catalog+schema.
		oldTable := tablename.TableName{Catalog: targetTn.Catalog, Schema: targetTn.Schema, Table: oldName}

		if err := d.exec(ctx, fmt.Sprintf("ALTER TABLE IF EXISTS %s RENAME TO %s", destination.QuoteTableName(targetTable), destination.QuoteIdentifier(oldName))); err != nil {
			config.Debug("[DUCKDB] No existing table to rename (this is OK for first run)")
		}

		if err := d.exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", destination.QuoteTableName(stagingTable), destination.QuoteIdentifier(targetName))); err != nil {
			return fmt.Errorf("failed to rename staging to target: %w", err)
		}

		_ = d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteDuckTable(oldTable)))
	} else {
		// Cross-schema swap: DuckDB's ALTER TABLE RENAME doesn't support cross-schema.
		// Recreate the target with the staging table's recorded schema (preserving
		// NOT NULL / PK constraints) and copy rows. The schema is looked up by the
		// staging table name so parallel multi-table PrepareTable calls don't race.
		sch := d.lookupSchema(stagingTable)
		if sch == nil {
			return fmt.Errorf("cannot swap %s -> %s: no recorded schema for staging table", stagingTable, targetTable)
		}

		// Replace only PrepareTables the staging side, so the target schema may
		// not exist yet (DuckDB doesn't auto-create "public" for fresh DBs).
		if err := d.ensureSchemaExistsLocked(ctx, targetTn); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}

		if err := d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(targetTable))); err != nil {
			return fmt.Errorf("failed to drop target table: %w", err)
		}

		createSQL := buildCreateTableSQL(destination.QuoteTableName(targetTable), sch.Columns, sch.PrimaryKeys)
		if err := d.exec(ctx, createSQL); err != nil {
			return fmt.Errorf("failed to recreate target table: %w", err)
		}

		quotedCols := make([]string, len(sch.Columns))
		for i, c := range sch.Columns {
			quotedCols[i] = destination.QuoteIdentifier(c.Name)
		}
		colList := strings.Join(quotedCols, ", ")
		copySQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
			destination.QuoteTableName(targetTable),
			colList, colList,
			destination.QuoteTableName(stagingTable))
		if err := d.exec(ctx, copySQL); err != nil {
			return fmt.Errorf("failed to copy staging rows into target: %w", err)
		}

		if err := d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(stagingTable))); err != nil {
			return fmt.Errorf("failed to drop staging table: %w", err)
		}
		d.forgetSchema(stagingTable)
	}
	if conditional {
		result, exists, err := d.cdcTargetIncarnationLocked(ctx, targetTable)
		if err != nil {
			return err
		}
		if !exists || result != opts.CDCExpectedResultIncarnation {
			return fmt.Errorf("DuckDB CDC target %s was replaced during conditional swap", targetTable)
		}
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func quoteDuckTable(tn tablename.TableName) string {
	result := destination.QuoteIdentifier(tn.Table)
	if tn.Schema != "" {
		result = destination.QuoteIdentifier(tn.Schema) + "." + result
	}
	if tn.Catalog != "" {
		result = destination.QuoteIdentifier(tn.Catalog) + "." + result
	}
	return result
}

func (d *DuckDBDestination) SupportsCDCConditionalSwap() bool { return true }

func (d *DuckDBDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetTn := duckTable(targetTable)
	stagingTn := duckTable(stagingTable)
	if !duckSameNamespace(stagingTn, targetTn) {
		return "", "", fmt.Errorf("DuckDB CDC conditional swaps require staging and target tables in the same schema")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	stagingIncarnation, exists, err := d.ensureCDCTargetIncarnationLocked(ctx, stagingTable)
	if err != nil {
		return "", "", err
	}
	if !exists || stagingIncarnation == "" {
		return "", "", fmt.Errorf("DuckDB CDC staging table %q has no durable physical incarnation", stagingTable)
	}
	metadata, _, err := d.duckDBTargetMetadataLocked(ctx, stagingTable)
	if err != nil {
		return "", "", err
	}
	marker := duckDBIncarnationMarker(metadata.incarnationComment)
	resultTable := metadata.table
	resultTable.Table = targetTn.Table
	return stagingIncarnation, duckDBTableIncarnation(resultTable, marker), nil
}

func (d *DuckDBDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()
	if err := d.mergeTableLocked(ctx, opts); err != nil {
		return err
	}
	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commit = true
	return nil
}

// MergeCDCTablesAtomically applies one CDC batch's staged changes to every
// target table inside a single transaction, so a crash cannot leave a torn
// cross-table state.
func (d *DuckDBDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin multi-table CDC transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()
	for _, merge := range merges {
		if merge.Truncate {
			if merge.Options.CDCExpectedIncarnation != "" {
				if err := d.validateCDCIncarnationLocked(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
					return err
				}
			}
			if err := d.exec(ctx, "DELETE FROM "+destination.QuoteTableName(merge.Options.TargetTable)); err != nil {
				return fmt.Errorf("failed to reset CDC target %s: %w", merge.Options.TargetTable, err)
			}
		}
		if err := d.mergeTableLocked(ctx, merge.Options); err != nil {
			return fmt.Errorf("failed to merge CDC target %s: %w", merge.Options.TargetTable, err)
		}
	}
	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit multi-table CDC transaction: %w", err)
	}
	commit = true
	return nil
}

func (d *DuckDBDestination) validateCDCIncarnationLocked(ctx context.Context, table, expectedIncarnation string) error {
	current, exists, err := d.cdcTargetIncarnationLocked(ctx, table)
	if err != nil {
		return err
	}
	if !exists || current != expectedIncarnation {
		return fmt.Errorf("DuckDB CDC target %s was replaced before mutation", table)
	}
	return nil
}

// mergeTableLocked runs the merge inside the caller's open transaction;
// d.mu must be held.
func (d *DuckDBDestination) mergeTableLocked(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	stagingColumns := opts.Columns
	destColumns := destination.DestinationColumns(stagingColumns)
	stagingQuoted := quoteColumns(stagingColumns)
	destQuoted := quoteColumns(destColumns)
	nonPKColumns := filterColumns(destColumns, opts.PrimaryKeys)

	if opts.CDCExpectedIncarnation != "" {
		if err := d.validateCDCIncarnationLocked(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
	}

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	primaryKeyCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")
	onCondition := destination.MergeJoinCondition(
		primaryKeyCondition,
		opts.IncrementalPredicate,
	)

	// Build dedup subquery to handle duplicate PKs in staging. For CDC data the
	// latest change per PK wins (LSN strings are fixed-width and sort
	// lexicographically); otherwise, when an incremental key is set the latest
	// row per PK wins, else arbitrary.
	quotedPKs := quoteColumns(opts.PrimaryKeys)
	isCDC := destination.HasCDCDeletedColumn(stagingColumns)
	// _cdc_unchanged_cols is only emitted by sources that can mark columns as
	// unchanged (e.g. Postgres TOAST); other CDC sources materialize full rows
	// and their staging tables have no such column to reference.
	applyUnchangedCols := isCDC && slices.Contains(stagingColumns, destination.CDCUnchangedColsColumn)
	dedupOrderBy := "(SELECT NULL)"
	if isCDC {
		dedupOrderBy = destination.CDCLatestOverallOrderBy(quoteIdentifier)
	} else if opts.IncrementalKey != "" {
		dedupOrderBy = destination.QuoteIdentifier(opts.IncrementalKey) + " DESC"
	}
	usedInternalNames := make(map[string]struct{}, len(stagingColumns)+6)
	for _, col := range stagingColumns {
		usedInternalNames[strings.ToLower(col)] = struct{}{}
	}
	uniqueInternalName := func(base string) string {
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := usedInternalNames[strings.ToLower(candidate)]; !exists {
				usedInternalNames[strings.ToLower(candidate)] = struct{}{}
				return candidate
			}
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
	}
	dedupRowNumber := quoteIdentifier(uniqueInternalName("__bruin_dedup_rn"))
	dedupSourceAs := func(where, orderBy, alias string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s%s) AS _numbered WHERE %s = 1) AS %s`,
			strings.Join(stagingQuoted, ", "),
			strings.Join(stagingQuoted, ", "),
			strings.Join(quotedPKs, ", "),
			orderBy,
			dedupRowNumber,
			destination.QuoteTableName(opts.StagingTable),
			where,
			dedupRowNumber,
			alias,
		)
	}
	dedupSource := func(where string) string { return dedupSourceAs(where, dedupOrderBy, "source") }

	// For CDC, updates use the latest active image. Inserts combine that image
	// with the latest overall CDC metadata; delete-only keys use their tombstone.
	updateSource := dedupSource("")
	insertSource := updateSource
	equalDeleteMarker := ""
	if isCDC {
		equalDeleteMarker = quoteIdentifier(uniqueInternalName("__ingestr_has_equal_lsn_delete"))
		activeSource := dedupSourceAs(` WHERE "_cdc_deleted" = false`, dedupOrderBy, "active")
		latestSource := dedupSourceAs("", dedupOrderBy, "latest")
		updateSource = fmt.Sprintf(
			`(SELECT active.*, COALESCE(latest."_cdc_lsn" = active."_cdc_lsn" AND latest."_cdc_deleted" = true, false) AS %s FROM %s LEFT JOIN %s ON %s) AS source`,
			equalDeleteMarker,
			activeSource,
			latestSource,
			buildJoinCondition(opts.PrimaryKeys, "active", "latest"),
		)
		imageRowNumber := quoteIdentifier(uniqueInternalName("__bruin_image_rn"))
		latestLSN := quoteIdentifier(uniqueInternalName("__ingestr_latest_lsn"))
		latestDeleted := quoteIdentifier(uniqueInternalName("__ingestr_latest_deleted"))
		latestSyncedAt := quoteIdentifier(uniqueInternalName("__ingestr_latest_synced_at"))
		insertColumns := make([]string, len(destColumns))
		for i, col := range destColumns {
			quoted := quoteIdentifier(col)
			switch {
			case strings.EqualFold(col, destination.CDCLSNColumn):
				insertColumns[i] = fmt.Sprintf("%s AS %s", latestLSN, quoted)
			case strings.EqualFold(col, destination.CDCDeletedColumn):
				insertColumns[i] = fmt.Sprintf("%s AS %s", latestDeleted, quoted)
			case strings.EqualFold(col, destination.CDCSyncedAtColumn):
				insertColumns[i] = fmt.Sprintf("%s AS %s", latestSyncedAt, quoted)
			default:
				insertColumns[i] = quoted
			}
		}
		insertSource = fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY "_cdc_deleted" ASC, "_cdc_lsn" DESC) AS %s, FIRST_VALUE("_cdc_lsn") OVER (PARTITION BY %s ORDER BY %s) AS %s, FIRST_VALUE("_cdc_deleted") OVER (PARTITION BY %s ORDER BY %s) AS %s, FIRST_VALUE("_cdc_synced_at") OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) AS _numbered WHERE %s = 1) AS source`,
			strings.Join(insertColumns, ", "),
			strings.Join(stagingQuoted, ", "),
			strings.Join(quotedPKs, ", "),
			imageRowNumber,
			strings.Join(quotedPKs, ", "),
			dedupOrderBy,
			latestLSN,
			strings.Join(quotedPKs, ", "),
			dedupOrderBy,
			latestDeleted,
			strings.Join(quotedPKs, ", "),
			dedupOrderBy,
			latestSyncedAt,
			destination.QuoteTableName(opts.StagingTable),
			imageRowNumber,
		)
	}
	insertCondition := onCondition
	if isCDC {
		insertCondition = primaryKeyCondition
	}

	targetHasRows := true
	if !isCDC {
		var err error
		targetHasRows, err = d.tableHasRowsLocked(ctx, quotedTargetTable)
		if err != nil {
			return fmt.Errorf("failed to check target table rows: %w", err)
		}
	}

	if !targetHasRows {
		stagingKeysUnique, err := d.stagingPrimaryKeysUniqueLocked(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to check staging primary key uniqueness: %w", err)
		}
		if stagingKeysUnique {
			config.Debug("[DUCKDB MERGE] Empty-target staging keys are unique; using direct INSERT")
			insertSource = fmt.Sprintf(`%s AS source`, destination.QuoteTableName(opts.StagingTable))
		} else {
			config.Debug("[DUCKDB MERGE] Empty-target staging keys need deduplication")
		}

		insertSQL := fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s`,
			quotedTargetTable,
			strings.Join(destQuoted, ", "),
			strings.Join(destQuoted, ", "),
			insertSource,
		)
		config.Debug("[DUCKDB MERGE] Executing empty-target INSERT: %s", insertSQL)

		if err := d.exec(ctx, insertSQL); err != nil {
			return fmt.Errorf("failed to insert new records: %w", err)
		}

		config.Debug("[DUCKDB MERGE] Merge completed in %v", time.Since(startMerge))
		return nil
	}

	runUpdate := func() error {
		if len(nonPKColumns) == 0 {
			return nil
		}
		updateCondition := onCondition
		if isCDC {
			updateCondition += fmt.Sprintf(` AND (target."_cdc_lsn" IS NULL OR source."_cdc_lsn" > target."_cdc_lsn" OR (source."_cdc_lsn" = target."_cdc_lsn" AND COALESCE(target."_cdc_deleted", false) = false AND source.%s))`, equalDeleteMarker)
		}
		updateSQL := fmt.Sprintf(
			`UPDATE %s AS target SET %s FROM %s WHERE %s`,
			quotedTargetTable,
			buildUpdateSet(nonPKColumns, "target", "source", applyUnchangedCols),
			updateSource,
			updateCondition,
		)
		config.Debug("[DUCKDB MERGE] Executing UPDATE: %s", updateSQL)

		if err := d.exec(ctx, updateSQL); err != nil {
			return fmt.Errorf("failed to update existing records: %w", err)
		}
		return nil
	}

	runInsert := func() error {
		insertSQL := fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
			quotedTargetTable,
			strings.Join(destQuoted, ", "),
			strings.Join(destQuoted, ", "),
			insertSource,
			quotedTargetTable,
			insertCondition,
		)
		config.Debug("[DUCKDB MERGE] Executing INSERT: %s", insertSQL)

		if err := d.exec(ctx, insertSQL); err != nil {
			return fmt.Errorf("failed to insert new records: %w", err)
		}
		return nil
	}

	// With a predicate, the INSERT runs first so its anti-join sees the
	// pre-update target: an UPDATE that moves a matched row out of the
	// predicate window would otherwise make the INSERT re-add it as a
	// duplicate. CDC anti-joins always use the primary key alone.
	steps := []func() error{runUpdate, runInsert}
	if strings.TrimSpace(opts.IncrementalPredicate) != "" {
		steps = []func() error{runInsert, runUpdate}
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	if isCDC {
		// Mark rows deleted only when the latest change for the PK is a delete,
		// carrying the delete's LSN so resume picks up after it.
		markDeletedSQL := fmt.Sprintf(
			`UPDATE %s AS target SET "_cdc_deleted" = true, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at" FROM %s WHERE %s AND source."_cdc_deleted" = true AND (target."_cdc_lsn" IS NULL OR source."_cdc_lsn" > target."_cdc_lsn" OR (source."_cdc_lsn" = target."_cdc_lsn" AND COALESCE(target."_cdc_deleted", false) = false))`,
			quotedTargetTable,
			dedupSource(""),
			onCondition,
		)
		config.Debug("[DUCKDB MERGE] Executing CDC delete marking: %s", markDeletedSQL)

		if err := d.exec(ctx, markDeletedSQL); err != nil {
			return fmt.Errorf("failed to mark deleted records: %w", err)
		}
	}

	config.Debug("[DUCKDB MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *DuckDBDestination) stagingPrimaryKeysUniqueLocked(ctx context.Context, opts destination.MergeOptions) (bool, error) {
	if len(opts.PrimaryKeys) == 0 {
		return false, nil
	}

	totalExpr := "COUNT(*)"
	nullChecks := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		nullChecks[i] = fmt.Sprintf("%s IS NULL", destination.QuoteIdentifier(pk))
	}

	distinctExpr := ""
	if len(opts.PrimaryKeys) == 1 {
		distinctExpr = fmt.Sprintf("COUNT(DISTINCT %s)", destination.QuoteIdentifier(opts.PrimaryKeys[0]))
	} else {
		distinctExpr = fmt.Sprintf("COUNT(DISTINCT (%s))", strings.Join(quoteColumns(opts.PrimaryKeys), ", "))
	}

	query := fmt.Sprintf(
		`SELECT %s AS total_rows, %s AS distinct_keys, COUNT(*) FILTER (WHERE %s) AS null_keys FROM %s`,
		totalExpr,
		distinctExpr,
		strings.Join(nullChecks, " OR "),
		destination.QuoteTableName(opts.StagingTable),
	)

	stmt, err := d.conn.NewStatement()
	if err != nil {
		return false, err
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(query); err != nil {
		config.LogFailedQuery(query, err)
		return false, err
	}

	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		config.LogFailedQuery(query, err)
		return false, err
	}
	defer reader.Release()

	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return false, err
		}
		return false, nil
	}

	batch := reader.RecordBatch()
	if batch.NumRows() == 0 || batch.NumCols() != 3 {
		return false, fmt.Errorf("unexpected uniqueness check result shape")
	}

	total, ok := int64ValueAt(batch.Column(0), 0)
	if !ok {
		return false, fmt.Errorf("unexpected total row count type %s", batch.Column(0).DataType())
	}
	distinctKeys, ok := int64ValueAt(batch.Column(1), 0)
	if !ok {
		return false, fmt.Errorf("unexpected distinct key count type %s", batch.Column(1).DataType())
	}
	nullKeys, ok := int64ValueAt(batch.Column(2), 0)
	if !ok {
		return false, fmt.Errorf("unexpected null key count type %s", batch.Column(2).DataType())
	}

	return nullKeys == 0 && total == distinctKeys, nil
}

func int64ValueAt(arr arrow.Array, index int) (int64, bool) {
	if arr.IsNull(index) {
		return 0, false
	}

	switch col := arr.(type) {
	case *array.Int64:
		return col.Value(index), true
	case *array.Uint64:
		v := col.Value(index)
		if v > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(v), true
	}
	return 0, false
}

func (d *DuckDBDestination) tableHasRowsLocked(ctx context.Context, quotedTable string) (bool, error) {
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return false, err
	}
	defer func() { _ = stmt.Close() }()

	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s LIMIT 1)", quotedTable)
	if err := stmt.SetSqlQuery(query); err != nil {
		config.LogFailedQuery(query, err)
		return false, err
	}

	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		config.LogFailedQuery(query, err)
		return false, err
	}
	defer reader.Release()

	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return false, err
		}
		return false, nil
	}

	batch := reader.RecordBatch()
	if batch.NumRows() == 0 || batch.NumCols() == 0 || batch.Column(0).IsNull(0) {
		return false, nil
	}
	if col, ok := batch.Column(0).(*array.Boolean); ok {
		return col.Value(0), nil
	}
	return false, fmt.Errorf("unexpected EXISTS result type %s", batch.Column(0).DataType())
}

func (d *DuckDBDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	deleteSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE %s >= ? AND %s <= ?`,
		quotedTargetTable, quoteIdentifier(opts.IncrementalKey), quoteIdentifier(opts.IncrementalKey),
	)
	config.Debug("[DUCKDB DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if err := d.exec(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quotedStagingTable, quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) %s`, quotedTargetTable, colList, selectClause)
	config.Debug("[DUCKDB DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if err := d.exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *DuckDBDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditions(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s AS target SET
			"_scd_valid_to" = source."_scd_valid_from",
			"_scd_is_current" = false
		FROM %s AS source
		WHERE %s
		  AND target."_scd_is_current" = true
		  AND (%s)`,
		quotedTargetTable,
		quotedStagingTable,
		onCondition,
		changeConditions,
	)
	config.Debug("[DUCKDB SCD2] Step 1 - Close changed records: %s", updateSQL)

	if err := d.exec(ctx, updateSQL); err != nil {
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s AS target SET
				"_scd_valid_to" = $1,
				"_scd_is_current" = false
			WHERE target."_scd_is_current" = true
			  AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
			quotedTargetTable,
			quotedStagingTable,
			onCondition,
		)
		config.Debug("[DUCKDB SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if err := d.exec(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	// Insert records that either:
	// - Don't exist at all (net-new)
	// - Exist but have changed (new version - the old version was closed in step 1)
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS target
			WHERE %s
			  AND target."_scd_is_current" = true
		)`,
		quotedTargetTable,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quotedStagingTable,
		quotedTargetTable,
		onCondition,
	)
	config.Debug("[DUCKDB SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if err := d.exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *DuckDBDestination) DropTable(ctx context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(table))); err != nil {
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DUCKDB] Dropped table: %s", table)
	return nil
}

func (d *DuckDBDestination) TruncateTable(ctx context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", destination.QuoteTableName(table))); err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DUCKDB] Truncated table: %s", table)
	return nil
}

func (d *DuckDBDestination) InsertFromStaging(ctx context.Context, opts destination.InsertFromStagingOptions) error {
	columns := quoteColumns(destination.DestinationColumns(opts.Columns))
	if len(columns) == 0 {
		return errors.New("insert from staging requires at least one column")
	}
	columnList := strings.Join(columns, ", ")
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM %s",
		destination.QuoteTableName(opts.TargetTable), columnList, columnList, destination.QuoteTableName(opts.StagingTable),
	)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert into table %s from staging: %w", opts.TargetTable, err)
	}
	return nil
}

func (d *DuckDBDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return fmt.Errorf("cannot conditionally truncate %s without a destination incarnation", table)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.exec(ctx, "BEGIN"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()
	current, exists, err := d.cdcTargetIncarnationLocked(ctx, table)
	if err != nil {
		return err
	}
	if !exists || current != expectedIncarnation {
		return fmt.Errorf("DuckDB CDC target %q physical incarnation changed", table)
	}
	if err := d.exec(ctx, fmt.Sprintf("DELETE FROM %s", destination.QuoteTableName(table))); err != nil {
		return fmt.Errorf("failed to conditionally clear DuckDB CDC target %s: %w", table, err)
	}
	if err := d.exec(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func (d *DuckDBDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.exec(ctx, sql, args...)
}

func (d *DuckDBDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	d.mu.Lock()
	if err := d.exec(ctx, "BEGIN"); err != nil {
		d.mu.Unlock()
		return nil, err
	}
	return &duckdbTransaction{d: d}, nil
}

type duckdbTransaction struct {
	d      *DuckDBDestination
	closed bool
}

func (t *duckdbTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if t.closed {
		return fmt.Errorf("transaction is closed")
	}
	return t.d.exec(ctx, sql, args...)
}

func (t *duckdbTransaction) Commit(ctx context.Context) error {
	if t.closed {
		return nil
	}
	t.closed = true
	defer t.d.mu.Unlock()
	return t.d.exec(ctx, "COMMIT")
}

func (t *duckdbTransaction) Rollback(ctx context.Context) error {
	if t.closed {
		return nil
	}
	t.closed = true
	defer t.d.mu.Unlock()
	return t.d.exec(ctx, "ROLLBACK")
}

func (d *DuckDBDestination) SupportsReplaceStrategy() bool      { return true }
func (d *DuckDBDestination) SupportsAppendStrategy() bool       { return true }
func (d *DuckDBDestination) SupportsMergeStrategy() bool        { return true }
func (d *DuckDBDestination) SupportsIncrementalPredicate() bool { return true }
func (d *DuckDBDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *DuckDBDestination) SupportsSCD2Strategy() bool         { return true }
func (d *DuckDBDestination) SupportsAtomicSwap() bool           { return true }
func (d *DuckDBDestination) SupportsCDCMerge() bool             { return true }
func (d *DuckDBDestination) SupportsCDCUnchangedCols() bool     { return true }
func (d *DuckDBDestination) SupportsCDCConditionalMerge() bool  { return true }

func (d *DuckDBDestination) ValidateManagedCDCState() error {
	if strings.HasPrefix(d.filePath, "md:") {
		return fmt.Errorf("MotherDuck does not expose a process-wide managed CDC run lease")
	}
	return nil
}

// GetScheme returns the primary URI scheme for DuckDB.
func (d *DuckDBDestination) GetScheme() string { return "duckdb" }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *DuckDBDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %s`, destination.QuoteTableName(table))
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return "", err
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(query); err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") {
			return "", nil
		}
		return "", err
	}

	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") ||
			strings.Contains(err.Error(), "not found") {
			return "", nil
		}
		return "", err
	}
	defer reader.Release()

	if reader.Next() {
		batch := reader.RecordBatch()
		if batch.NumRows() > 0 && batch.NumCols() > 0 {
			col := batch.Column(0)
			if col.IsNull(0) {
				return "", nil
			}
			if strCol, ok := col.(*array.String); ok {
				// strCol.Value aliases the Arrow buffer, which the deferred
				// reader.Release() frees before this string is used; clone it
				// so the resume LSN survives the release (see GetTableSchema).
				return strings.Clone(strCol.Value(0)), nil
			}
		}
	}

	return "", nil
}

func (d *DuckDBDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	connectorLiteral := strings.ReplaceAll(connectorID, "'", "''")
	query := fmt.Sprintf(`SELECT "event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn" FROM %s WHERE "connector_id" = '%s'`,
		destination.QuoteTableName(table), connectorLiteral)
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return nil, err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(query); err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "Catalog Error") {
			return nil, nil
		}
		return nil, err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "Catalog Error") || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	defer reader.Release()

	var entries []destination.CDCStateEntry
	for reader.Next() {
		batch := reader.RecordBatch()
		eventIDs, eventOK := batch.Column(0).(*array.String)
		sourceTables, sourceOK := batch.Column(1).(*array.String)
		destinationTables, destinationOK := batch.Column(2).(*array.String)
		kinds, kindOK := batch.Column(3).(*array.String)
		statuses, statusOK := batch.Column(5).(*array.String)
		positions, positionOK := batch.Column(6).(*array.String)
		if !eventOK || !sourceOK || !destinationOK || !kindOK || !statusOK || !positionOK {
			return nil, fmt.Errorf("unexpected DuckDB CDC state column types")
		}
		for row := 0; row < int(batch.NumRows()); row++ {
			generation, ok := int64ValueAt(batch.Column(4), row)
			if !ok {
				return nil, fmt.Errorf("unexpected DuckDB CDC state generation type %s", batch.Column(4).DataType())
			}
			entries = append(entries, destination.CDCStateEntry{
				EventID:          strings.Clone(eventIDs.Value(row)),
				SourceTable:      strings.Clone(sourceTables.Value(row)),
				DestinationTable: strings.Clone(destinationTables.Value(row)),
				StateKind:        strings.Clone(kinds.Value(row)),
				Generation:       generation,
				Status:           strings.Clone(statuses.Value(row)),
				Position:         strings.Clone(positions.Value(row)),
			})
		}
	}
	return entries, reader.Err()
}

func (d *DuckDBDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	tn := duckTable(claim.DestinationTable)
	d.mu.Lock()
	defer d.mu.Unlock()
	if tn.Catalog == "" {
		if d.catalog == "" {
			catalog, err := d.currentCatalog(ctx)
			if err != nil {
				return err
			}
			d.catalog = catalog
		}
		tn.Catalog = d.catalog
	}
	canonicalTarget := canonicalDuckDBTarget(tn)
	targetLiteral := strings.ReplaceAll(canonicalTarget, "'", "''")
	connectorLiteral := strings.ReplaceAll(ownerID, "'", "''")
	query := fmt.Sprintf(`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES ('%s', '%s', CURRENT_TIMESTAMP)
		ON CONFLICT ("destination_table") DO UPDATE SET "claimed_at" = "claimed_at" RETURNING "connector_id"`, destination.QuoteTableName(claimTable), targetLiteral, connectorLiteral)
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(query); err != nil {
		return err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return err
	}
	defer reader.Release()
	if !reader.Next() || reader.RecordBatch().NumRows() != 1 {
		return fmt.Errorf("CDC target claim for %q returned no owner", canonicalTarget)
	}
	owners, ok := reader.RecordBatch().Column(0).(*array.String)
	if !ok {
		return fmt.Errorf("unexpected DuckDB CDC target owner type")
	}
	owner := strings.Clone(owners.Value(0))
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return reader.Err()
}

func (d *DuckDBDestination) ClaimAndPrepareEmptyCDCTarget(
	ctx context.Context,
	claimTable string,
	claim destination.CDCTargetClaim,
	opts destination.PrepareOptions,
) (string, error) {
	if opts.Schema == nil {
		return "", fmt.Errorf("cannot create an empty managed CDC target without a schema")
	}
	ownerID, err := claim.OwnerID()
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	targetTn := duckTable(opts.Table)
	claimTn := duckTable(claim.DestinationTable)
	if d.catalog == "" {
		catalog, err := d.currentCatalog(ctx)
		if err != nil {
			return "", err
		}
		d.catalog = catalog
	}
	if targetTn.Catalog == "" {
		targetTn.Catalog = d.catalog
	}
	if claimTn.Catalog == "" {
		claimTn.Catalog = d.catalog
	}
	if canonicalDuckDBTarget(targetTn) != canonicalDuckDBTarget(claimTn) {
		return "", fmt.Errorf("CDC target claim %q does not match prepared table %q", claim.DestinationTable, opts.Table)
	}
	if err := d.exec(ctx, "BEGIN"); err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()
	if err := d.ensureSchemaExistsLocked(ctx, targetTn); err != nil {
		return "", err
	}
	if _, exists, err := d.cdcTargetIncarnationLocked(ctx, opts.Table); err != nil {
		return "", err
	} else if exists {
		return "", fmt.Errorf("destination table %q already exists", opts.Table)
	}
	createSQL := strings.Replace(
		buildCreateTableSQL(destination.QuoteTableName(opts.Table), opts.Schema.Columns, opts.PrimaryKeys),
		"CREATE TABLE IF NOT EXISTS", "CREATE TABLE", 1,
	)
	if err := d.exec(ctx, createSQL); err != nil {
		return "", fmt.Errorf("failed to exclusively create DuckDB CDC target: %w", err)
	}
	canonicalTarget := canonicalDuckDBTarget(targetTn)
	query := fmt.Sprintf(
		`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES ('%s', '%s', CURRENT_TIMESTAMP)
		ON CONFLICT ("destination_table") DO UPDATE SET "claimed_at" = "claimed_at" RETURNING "connector_id"`,
		destination.QuoteTableName(claimTable),
		strings.ReplaceAll(canonicalTarget, "'", "''"),
		strings.ReplaceAll(ownerID, "'", "''"),
	)
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return "", err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(query); err != nil {
		return "", err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return "", err
	}
	if !reader.Next() || reader.RecordBatch().NumRows() != 1 {
		reader.Release()
		return "", fmt.Errorf("CDC target claim for %q returned no owner", canonicalTarget)
	}
	owners, ok := reader.RecordBatch().Column(0).(*array.String)
	if !ok {
		reader.Release()
		return "", fmt.Errorf("unexpected DuckDB CDC target owner type")
	}
	owner := strings.Clone(owners.Value(0))
	reader.Release()
	if owner != ownerID {
		return "", fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	incarnation, exists, err := d.ensureCDCTargetIncarnationLocked(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	if !exists || incarnation == "" {
		return "", fmt.Errorf("claimed DuckDB CDC target %q has no durable physical incarnation", opts.Table)
	}
	if err := d.exec(ctx, "COMMIT"); err != nil {
		return "", err
	}
	committed = true
	d.recordSchema(opts.Table, opts.Schema, opts.PrimaryKeys)
	return incarnation, nil
}

func (d *DuckDBDestination) CanonicalCDCTarget(ctx context.Context, table string) (string, error) {
	tn := duckTable(table)
	d.mu.Lock()
	defer d.mu.Unlock()
	if tn.Catalog == "" {
		if d.catalog == "" {
			catalog, err := d.currentCatalog(ctx)
			if err != nil {
				return "", err
			}
			d.catalog = catalog
		}
		tn.Catalog = d.catalog
	}
	return canonicalDuckDBTarget(tn), nil
}

func canonicalDuckDBTarget(tn tablename.TableName) string {
	return destination.CDCTargetKey(strings.ToLower(tn.Catalog), strings.ToLower(canonicalDuckDBSchema(tn.Schema)), strings.ToLower(tn.Table))
}

func (d *DuckDBDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cdcTargetIncarnationLocked(ctx, table)
}

func (d *DuckDBDestination) cdcTargetIncarnationLocked(ctx context.Context, table string) (string, bool, error) {
	metadata, exists, err := d.duckDBTargetMetadataLocked(ctx, table)
	if err != nil || !exists {
		return "", exists, err
	}
	marker := duckDBIncarnationMarker(metadata.incarnationComment)
	if marker == "" {
		return "", true, nil
	}
	return duckDBTableIncarnation(metadata.table, marker), true, nil
}

type duckDBTargetMetadata struct {
	table                tablename.TableName
	incarnationComment   string
	hasIncarnationColumn bool
}

func (d *DuckDBDestination) duckDBTargetMetadataLocked(ctx context.Context, table string) (duckDBTargetMetadata, bool, error) {
	tn := duckTable(table)
	if tn.Catalog == "" {
		if d.catalog == "" {
			catalog, err := d.currentCatalog(ctx)
			if err != nil {
				return duckDBTargetMetadata{}, false, err
			}
			d.catalog = catalog
		}
		tn.Catalog = d.catalog
	}
	tn.Schema = canonicalDuckDBSchema(tn.Schema)
	literal := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	query := fmt.Sprintf(`SELECT tables.database_name, tables.schema_name, tables.table_name,
			COALESCE(columns.comment, ''), columns.column_name IS NOT NULL
		FROM duckdb_tables() AS tables
		LEFT JOIN duckdb_columns() AS columns
			ON lower(columns.database_name) = lower(tables.database_name)
			AND lower(columns.schema_name) = lower(tables.schema_name)
			AND lower(columns.table_name) = lower(tables.table_name)
			AND lower(columns.column_name) = lower('%s')
		WHERE lower(tables.database_name) = lower('%s')
			AND lower(tables.schema_name) = lower('%s')
			AND lower(tables.table_name) = lower('%s')`,
		literal(destination.CDCLSNColumn), literal(tn.Catalog), literal(tn.Schema), literal(tn.Table))
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return duckDBTargetMetadata{}, false, err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(query); err != nil {
		return duckDBTargetMetadata{}, false, err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return duckDBTargetMetadata{}, false, fmt.Errorf("failed to read DuckDB CDC target incarnation for %s: %w", table, err)
	}
	defer reader.Release()
	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return duckDBTargetMetadata{}, false, err
		}
		return duckDBTargetMetadata{}, false, nil
	}
	batch := reader.RecordBatch()
	if batch.NumRows() == 0 {
		return duckDBTargetMetadata{}, false, nil
	}
	databaseNames, databaseOK := batch.Column(0).(*array.String)
	schemaNames, schemaOK := batch.Column(1).(*array.String)
	tableNames, tableOK := batch.Column(2).(*array.String)
	comments, commentOK := batch.Column(3).(*array.String)
	hasIncarnationColumns, hasIncarnationColumnOK := batch.Column(4).(*array.Boolean)
	if !databaseOK || !schemaOK || !tableOK || !commentOK || !hasIncarnationColumnOK {
		return duckDBTargetMetadata{}, false, fmt.Errorf("unexpected DuckDB CDC target incarnation column types")
	}
	return duckDBTargetMetadata{
		table: tablename.TableName{
			Catalog: strings.Clone(databaseNames.Value(0)),
			Schema:  strings.Clone(schemaNames.Value(0)),
			Table:   strings.Clone(tableNames.Value(0)),
		},
		incarnationComment:   strings.Clone(comments.Value(0)),
		hasIncarnationColumn: hasIncarnationColumns.Value(0),
	}, true, nil
}

const duckDBIncarnationCommentPrefix = "__ingestr_cdc_incarnation="

func duckDBIncarnationMarker(comment string) string {
	marker, found := strings.CutPrefix(comment, duckDBIncarnationCommentPrefix)
	if !found || len(marker) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(marker); err != nil {
		return ""
	}
	return marker
}

func duckDBTableIncarnation(table tablename.TableName, marker string) string {
	return destination.CDCTargetKey(
		strings.ToLower(table.Catalog),
		strings.ToLower(canonicalDuckDBSchema(table.Schema)),
		strings.ToLower(table.Table),
		marker,
	)
}

func (d *DuckDBDestination) EnsureCDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ensureCDCTargetIncarnationLocked(ctx, table)
}

func (d *DuckDBDestination) ensureCDCTargetIncarnationLocked(ctx context.Context, table string) (string, bool, error) {
	metadata, exists, err := d.duckDBTargetMetadataLocked(ctx, table)
	if err != nil || !exists {
		return "", exists, err
	}
	if !metadata.hasIncarnationColumn {
		return "", true, fmt.Errorf("DuckDB CDC target %q is missing managed column %q", table, destination.CDCLSNColumn)
	}
	marker := duckDBIncarnationMarker(metadata.incarnationComment)
	if marker == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", false, err
		}
		marker = hex.EncodeToString(token[:])
		query := fmt.Sprintf(
			"COMMENT ON COLUMN %s.%s IS '%s'",
			quoteDuckTable(metadata.table),
			destination.QuoteIdentifier(destination.CDCLSNColumn),
			duckDBIncarnationCommentPrefix+marker,
		)
		if err := d.exec(ctx, query); err != nil {
			return "", false, fmt.Errorf("failed to establish DuckDB CDC target incarnation for %s: %w", table, err)
		}
	}
	return duckDBTableIncarnation(metadata.table, marker), true, nil
}

func (d *DuckDBDestination) currentCatalog(ctx context.Context) (string, error) {
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return "", err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery("SELECT current_database()"); err != nil {
		return "", err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Release()
	if !reader.Next() || reader.RecordBatch().NumRows() != 1 {
		return "", fmt.Errorf("DuckDB current database query returned no result")
	}
	values, ok := reader.RecordBatch().Column(0).(*array.String)
	if !ok {
		return "", fmt.Errorf("unexpected DuckDB current database type")
	}
	return strings.Clone(values.Value(0)), reader.Err()
}

func (d *DuckDBDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	connectorLiteral := strings.ReplaceAll(connectorID, "'", "''")
	quotedTable := destination.QuoteTableName(table)
	query := fmt.Sprintf(`SELECT DISTINCT "event_id", "state_generation" FROM %s WHERE "connector_id" = '%s' AND "state_kind" = 'run' AND "state_generation" = (SELECT MAX("state_generation") FROM %s WHERE "connector_id" = '%s' AND "state_kind" = 'run') ORDER BY "event_id"`, quotedTable, connectorLiteral, quotedTable, connectorLiteral)
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(query); err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "Catalog Error") {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "Catalog Error") || strings.Contains(err.Error(), "not found") {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	defer reader.Release()

	var fence destination.CDCStateFence
	for reader.Next() {
		batch := reader.RecordBatch()
		eventIDs, ok := batch.Column(0).(*array.String)
		if !ok {
			return destination.CDCStateFence{}, fmt.Errorf("unexpected DuckDB CDC fence event ID type %s", batch.Column(0).DataType())
		}
		for row := 0; row < int(batch.NumRows()); row++ {
			generation, ok := int64ValueAt(batch.Column(1), row)
			if !ok {
				return destination.CDCStateFence{}, fmt.Errorf("unexpected DuckDB CDC fence generation type %s", batch.Column(1).DataType())
			}
			fence.Generation = generation
			fence.RunEventIDs = append(fence.RunEventIDs, strings.Clone(eventIDs.Value(row)))
		}
	}
	return fence, reader.Err()
}

func (d *DuckDBDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = "?"
		args = append(args, eventID)
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE "connector_id" = ? AND "event_id" IN (%s)`, destination.QuoteTableName(table), strings.Join(placeholders, ", "))
	return d.Exec(ctx, query, args...)
}

// GetTableSchema returns the current schema of a table, or nil if table doesn't exist.
func (d *DuckDBDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getTableSchemaLocked(ctx, table)
}

func (d *DuckDBDestination) getTableSchemaLocked(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseSchemaTable(table)

	query := fmt.Sprintf("DESCRIBE %s", destination.QuoteTableName(table))
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return nil, fmt.Errorf("failed to create statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(query); err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to set query: %w", err)
	}

	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer reader.Release()

	var columns []schema.Column
	var primaryKeys []string
	for reader.Next() {
		batch := reader.RecordBatch()
		for i := int64(0); i < batch.NumRows(); i++ {
			row := int(i)
			colName := batch.Column(0).(*array.String).Value(row)
			colType := batch.Column(1).(*array.String).Value(int(i))
			nullable := true
			if batch.NumCols() > 2 {
				nullStr := batch.Column(2).(*array.String).Value(row)
				nullable = nullStr == "YES"
			}
			isPrimaryKey := false
			if batch.NumCols() > 3 && !batch.Column(3).IsNull(row) {
				key := batch.Column(3).(*array.String).Value(row)
				isPrimaryKey = key == "PRI"
			}
			if isPrimaryKey {
				primaryKeys = append(primaryKeys, strings.Clone(colName))
			}

			dataType := mapDuckDBTypeToSchema(colType)
			precision, scale := 0, 0
			if dataType == schema.TypeDecimal {
				precision, scale = duckDBDecimalPrecisionScale(colType)
			}
			columns = append(columns, schema.Column{
				Name:         strings.Clone(colName),
				DataType:     dataType,
				Nullable:     nullable,
				IsPrimaryKey: isPrimaryKey,
				Precision:    precision,
				Scale:        scale,
			})
		}
	}

	if len(columns) == 0 {
		return nil, nil
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func mapDuckDBTypeToSchema(colType string) schema.DataType {
	colType = strings.ToUpper(colType)

	switch {
	case colType == "BOOLEAN" || colType == "BOOL":
		return schema.TypeBoolean
	case colType == "TINYINT" || colType == "INT1":
		return schema.TypeInt16
	case colType == "SMALLINT" || colType == "INT2":
		return schema.TypeInt16
	case colType == "INTEGER" || colType == "INT4" || colType == "INT":
		return schema.TypeInt32
	case colType == "BIGINT" || colType == "INT8":
		return schema.TypeInt64
	case colType == "REAL" || colType == "FLOAT4" || colType == "FLOAT":
		return schema.TypeFloat32
	case colType == "DOUBLE" || colType == "FLOAT8":
		return schema.TypeFloat64
	case strings.HasPrefix(colType, "DECIMAL") || strings.HasPrefix(colType, "NUMERIC"):
		return schema.TypeDecimal
	case colType == "VARCHAR" || colType == "TEXT" || colType == "STRING" || strings.HasPrefix(colType, "VARCHAR"):
		return schema.TypeString
	case colType == "BLOB" || colType == "BYTEA":
		return schema.TypeBinary
	case colType == "DATE":
		return schema.TypeDate
	case colType == "TIME":
		return schema.TypeTime
	case colType == "TIMESTAMP" || colType == "DATETIME":
		return schema.TypeTimestamp
	case colType == "TIMESTAMPTZ" || colType == "TIMESTAMP WITH TIME ZONE":
		return schema.TypeTimestampTZ
	case colType == "INTERVAL":
		return schema.TypeInterval
	case colType == "JSON":
		return schema.TypeJSON
	case colType == "UUID":
		return schema.TypeUUID
	case strings.HasSuffix(colType, "[]") || strings.HasPrefix(colType, "ARRAY"):
		return schema.TypeArray
	default:
		return schema.TypeString
	}
}

func duckDBDecimalPrecisionScale(colType string) (int, int) {
	upper := strings.ToUpper(strings.TrimSpace(colType))
	start := strings.IndexByte(upper, '(')
	end := strings.LastIndexByte(upper, ')')
	if start < 0 || end <= start+1 {
		return 0, 0
	}
	parts := strings.Split(upper[start+1:end], ",")
	if len(parts) != 2 {
		return 0, 0
	}
	precision, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0
	}
	scale, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0
	}
	return precision, scale
}

func parseDuckDBPath(uri string) (string, error) {
	if uri == "duckdb://:memory:" || uri == "duckdb:///:memory:" {
		return ":memory:", nil
	}

	if strings.HasPrefix(uri, "motherduck://") || strings.HasPrefix(uri, "md://") {
		return parseMotherDuckURI(uri)
	}

	if !strings.HasPrefix(uri, "duckdb://") {
		return "", fmt.Errorf("invalid duckdb URI: %s", uri)
	}

	path := strings.TrimPrefix(uri, "duckdb://")
	if path == "" {
		return ":memory:", nil
	}

	// Normalize accidental extra leading slash for absolute paths, e.g.
	// fmt.Sprintf("duckdb:///%s", "/tmp/x.duckdb") -> "duckdb:////tmp/x.duckdb".
	for strings.HasPrefix(path, "//") && (len(path) <= 3 || path[3] != ':') {
		path = path[1:]
	}

	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	if strings.HasPrefix(path, "/") && !strings.Contains(path[1:], "/") {
		path = "." + path
	}

	return path, nil
}

func parseMotherDuckURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse MotherDuck URI: %w", err)
	}

	token := parsed.Query().Get("token")
	if token == "" {
		return "", fmt.Errorf("MotherDuck token is required (use ?token=<your-token> in URI)")
	}

	database := strings.TrimPrefix(parsed.Host+parsed.Path, "/")
	database = strings.TrimPrefix(database, "/")

	if database == "" {
		return fmt.Sprintf("md:?motherduck_token=%s", token), nil
	}
	return fmt.Sprintf("md:%s?motherduck_token=%s", database, token), nil
}

// duckTable parses a possibly catalog-qualified DuckDB table name
// (catalog.schema.table), where the catalog is an attached database.
func duckTable(table string) tablename.TableName {
	tn, err := tablename.DuckDB.Parse(table, tablename.Defaults{})
	if err != nil {
		parts := strings.SplitN(table, ".", 2)
		if len(parts) == 2 {
			return tablename.TableName{Schema: parts[0], Table: parts[1]}
		}
		return tablename.TableName{Table: table}
	}
	return tn
}

func parseSchemaTable(table string) (string, string) {
	tn := duckTable(table)
	return tn.Schema, tn.Table
}

// duckSameNamespace reports whether two tables live in the same catalog+schema,
// which is required for DuckDB's ALTER TABLE ... RENAME (it cannot move a table
// across schemas or catalogs).
func duckSameNamespace(left, right tablename.TableName) bool {
	return canonicalDuckDBSchema(left.Schema) == canonicalDuckDBSchema(right.Schema) &&
		left.Catalog == right.Catalog
}

func canonicalDuckDBSchema(name string) string {
	if name == "" {
		return "main"
	}
	return name
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToDuckDB(col)
		colDefs = append(colDefs, fmt.Sprintf(`%s %s`, quoteIdentifier(col.Name), colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", table, strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = quoteIdentifier(k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

func quoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteIdentifier(col)
	}
	return quoted
}

func filterColumns(columns []string, exclude []string) []string {
	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var result []string
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col)] {
			result = append(result, col)
		}
	}
	return result
}

func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf(`%s.%s = %s.%s`, targetAlias, quoteIdentifier(key), sourceAlias, quoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

func buildUpdateSet(columns []string, targetAlias, sourceAlias string, cdcMerge bool) string {
	unchangedRef := fmt.Sprintf(`%s.%s`, sourceAlias, quoteIdentifier(destination.CDCUnchangedColsColumn))
	sets := make([]string, len(columns))
	for i, col := range columns {
		if cdcMerge && !destination.IsCDCMetaColumn(col) {
			sets[i] = cdcMergeAssign(
				col,
				fmt.Sprintf(`%s.%s`, targetAlias, quoteIdentifier(col)),
				fmt.Sprintf(`%s.%s`, sourceAlias, quoteIdentifier(col)),
				unchangedRef,
			)
		} else {
			sets[i] = fmt.Sprintf(`%s = %s.%s`, quoteIdentifier(col), sourceAlias, quoteIdentifier(col))
		}
	}
	return strings.Join(sets, ", ")
}

func cdcUnchangedColJSONNeedle(colName string) string {
	b, _ := json.Marshal([]string{colName})
	return strings.ReplaceAll(string(b), "'", "''")
}

func cdcMergeAssign(col, targetExpr, sourceExpr, unchangedColsExpr string) string {
	needle := cdcUnchangedColJSONNeedle(col)
	return fmt.Sprintf(
		`"%s" = CASE WHEN json_contains(%s, '%s') THEN %s ELSE %s END`,
		col, unchangedColsExpr, needle, targetExpr, sourceExpr,
	)
}

// buildChangeConditions builds change detection conditions using IS DISTINCT FROM.
func buildChangeConditions(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "false"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		conditions[i] = fmt.Sprintf(`%s.%s IS DISTINCT FROM %s.%s`, targetAlias, quoteIdentifier(col), sourceAlias, quoteIdentifier(col))
	}
	return strings.Join(conditions, " OR ")
}

func (d *DuckDBDestination) exec(ctx context.Context, sql string, args ...interface{}) error {
	if d.conn == nil {
		return fmt.Errorf("DuckDB destination not connected")
	}

	stmt, err := d.conn.NewStatement()
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(sql); err != nil {
		config.LogFailedQuery(sql, err)
		return err
	}

	if len(args) > 0 {
		if err := stmt.Prepare(ctx); err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
		params, err := buildParameterRecord(args)
		if err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
		defer params.Release()

		if err := stmt.Bind(ctx, params); err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
	}

	if len(args) == 0 {
		rdr, _, qerr := stmt.ExecuteQuery(ctx)
		if qerr == nil {
			defer rdr.Release()
			for rdr.Next() {
				// drain
			}
			if err := rdr.Err(); err != nil {
				config.LogFailedQuery(sql, err)
				return err
			}
			return nil
		}
		_, uerr := stmt.ExecuteUpdate(ctx)
		if uerr == nil {
			return nil
		}
		config.LogFailedQuery(sql, qerr)
		return qerr
	}

	_, uerr := stmt.ExecuteUpdate(ctx)
	if uerr == nil {
		return nil
	}
	rdr, _, qerr := stmt.ExecuteQuery(ctx)
	if qerr == nil {
		defer rdr.Release()
		for rdr.Next() {
			// drain
		}
		if err := rdr.Err(); err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
		return nil
	}
	config.LogFailedQuery(sql, uerr)
	return uerr
}

func buildParameterRecord(args []interface{}) (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, len(args))
	values := make([]interface{}, len(args))
	valid := make([]bool, len(args))

	for i, a := range args {
		switch v := a.(type) {
		case nil:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.String, Nullable: true}
			values[i] = nil
			valid[i] = false
		case *time.Time:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true}
			if v == nil {
				values[i] = nil
				valid[i] = false
			} else {
				values[i] = *v
				valid[i] = true
			}
		case time.Time:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true}
			values[i] = v
			valid[i] = true
		case bool:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.FixedWidthTypes.Boolean, Nullable: true}
			values[i] = v
			valid[i] = true
		case int:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Int64, Nullable: true}
			values[i] = int64(v)
			valid[i] = true
		case int32:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Int64, Nullable: true}
			values[i] = int64(v)
			valid[i] = true
		case int64:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Int64, Nullable: true}
			values[i] = v
			valid[i] = true
		case uint:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Uint64, Nullable: true}
			values[i] = uint64(v)
			valid[i] = true
		case uint32:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Uint64, Nullable: true}
			values[i] = uint64(v)
			valid[i] = true
		case uint64:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Uint64, Nullable: true}
			values[i] = v
			valid[i] = true
		case float32:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Float64, Nullable: true}
			values[i] = float64(v)
			valid[i] = true
		case float64:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Float64, Nullable: true}
			values[i] = v
			valid[i] = true
		case string:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.String, Nullable: true}
			values[i] = v
			valid[i] = true
		case []byte:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.Binary, Nullable: true}
			values[i] = v
			valid[i] = true
		default:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.String, Nullable: true}
			values[i] = fmt.Sprintf("%v", v)
			valid[i] = true
		}
	}

	schema := arrow.NewSchema(fields, nil)
	b := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer b.Release()

	for i, field := range fields {
		switch field.Type.ID() {
		case arrow.BOOL:
			builder := b.Field(i).(*array.BooleanBuilder)
			if valid[i] {
				builder.Append(values[i].(bool))
			} else {
				builder.AppendNull()
			}
		case arrow.INT64:
			builder := b.Field(i).(*array.Int64Builder)
			if valid[i] {
				builder.Append(values[i].(int64))
			} else {
				builder.AppendNull()
			}
		case arrow.UINT64:
			builder := b.Field(i).(*array.Uint64Builder)
			if valid[i] {
				builder.Append(values[i].(uint64))
			} else {
				builder.AppendNull()
			}
		case arrow.FLOAT64:
			builder := b.Field(i).(*array.Float64Builder)
			if valid[i] {
				builder.Append(values[i].(float64))
			} else {
				builder.AppendNull()
			}
		case arrow.STRING:
			builder := b.Field(i).(*array.StringBuilder)
			if valid[i] {
				builder.Append(values[i].(string))
			} else {
				builder.AppendNull()
			}
		case arrow.BINARY:
			builder := b.Field(i).(*array.BinaryBuilder)
			if valid[i] {
				builder.Append(values[i].([]byte))
			} else {
				builder.AppendNull()
			}
		case arrow.TIMESTAMP:
			builder := b.Field(i).(*array.TimestampBuilder)
			if valid[i] {
				t := values[i].(time.Time)
				builder.Append(arrow.Timestamp(t.UnixMicro()))
			} else {
				builder.AppendNull()
			}
		default:
			builder := b.Field(i).(*array.StringBuilder)
			if valid[i] {
				builder.Append(values[i].(string))
			} else {
				builder.AppendNull()
			}
		}
	}

	return b.NewRecordBatch(), nil
}

func errorsJoin(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return fmt.Errorf("%v; %w", a, b)
}
