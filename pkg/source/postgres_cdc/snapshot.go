package postgres_cdc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Snapshot struct {
	source      *PostgresCDCSource
	tableName   string
	tableSchema *schema.TableSchema
	cdcConfig   CDCConfig
	slotName    string
}

func NewSnapshot(src *PostgresCDCSource, tableName string, tableSchema *schema.TableSchema, cdcConfig CDCConfig, slotSuffix string) (*Snapshot, error) {
	slotName := cdcConfig.SlotName
	if slotName == "" {
		slotName = generateSlotName(tableName, cdcConfig.Publication, slotSuffix)
	}

	return &Snapshot{
		source:      src,
		tableName:   tableName,
		tableSchema: tableSchema,
		cdcConfig:   cdcConfig,
		slotName:    slotName,
	}, nil
}

// generateSlotName creates a stable, deterministic slot name based on table, publication, and an optional suffix.
func generateSlotName(tableName, publication, suffix string) string {
	normalizedTable := strings.ReplaceAll(tableName, ".", "_")
	name := fmt.Sprintf("ingestr_%s_%s", normalizedTable, publication)
	if suffix != "" {
		name = fmt.Sprintf("%s_%s", name, suffix)
	}
	return truncateSlotName(name)
}

func generateLegacySlotName(tableName, publication, suffix string) string {
	normalizedTable := strings.ReplaceAll(tableName, ".", "_")
	name := fmt.Sprintf("ingestr_%s_%s", normalizedTable, publication)
	if suffix != "" {
		name = fmt.Sprintf("%s_%s", name, suffix)
	}
	return truncateLegacySlotName(name)
}

func legacySlotNameUnambiguous(slotName, suffix string) bool {
	return suffix != "" && strings.HasSuffix(slotName, "_"+suffix)
}

func truncateLegacySlotName(name string) string {
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

// truncateSlotName enforces PostgreSQL's 63-character limit while preserving
// uniqueness from the full name, including its connector-specific suffix.
func truncateSlotName(name string) string {
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	hashSuffix := fmt.Sprintf("_%x", sum[:10])
	return name[:63-len(hashSuffix)] + hashSuffix
}

// Execute runs the snapshot and returns the LSN and slot name for the replicator to use
func (s *Snapshot) Execute(ctx context.Context, results chan<- source.RecordBatchResult, opts source.ReadOptions) (pglogrepl.LSN, string, error) {
	// Check if slot already exists (from a previous failed run)
	existingLSN, exists, err := checkSlotExists(ctx, s.source.queryPool, s.slotName)
	if err != nil {
		return 0, "", fmt.Errorf("failed to check existing slot: %w", err)
	}

	if exists {
		// Slot exists from previous run - drop it and recreate to get fresh snapshot
		config.Debug("[CDC] Dropping existing slot %s to get fresh snapshot", s.slotName)
		if err := s.dropSlot(ctx); err != nil {
			return 0, "", fmt.Errorf("failed to drop existing slot: %w", err)
		}
		config.Debug("[CDC] Previous slot LSN was: %s", existingLSN)
	}

	config.Debug("[CDC] Creating persistent replication slot: %s", s.slotName)

	// Create PERSISTENT replication slot and get the snapshot name
	// SnapshotAction must be "EXPORT_SNAPSHOT" to get a snapshot we can use with SET TRANSACTION SNAPSHOT
	result, err := pglogrepl.CreateReplicationSlot(
		ctx,
		s.source.replConn,
		s.slotName,
		"pgoutput",
		pglogrepl.CreateReplicationSlotOptions{
			Temporary:      false, // Persistent slot for incremental CDC
			SnapshotAction: "EXPORT_SNAPSHOT",
			Mode:           pglogrepl.LogicalReplication,
		},
	)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create replication slot: %w", err)
	}

	snapshotLSN, err := pglogrepl.ParseLSN(result.ConsistentPoint)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse LSN: %w", err)
	}

	config.Debug("[CDC] Persistent replication slot created: %s, LSN: %s, Snapshot: %s",
		s.slotName, result.ConsistentPoint, result.SnapshotName)

	// Use the snapshot for consistent read
	if err := s.readWithSnapshot(ctx, result.SnapshotName, snapshotLSN, results, opts); err != nil {
		return 0, "", fmt.Errorf("failed to read with snapshot: %w", err)
	}

	return snapshotLSN, s.slotName, nil
}

// checkSlotExists checks if a replication slot exists and returns its confirmed flush LSN.
func checkSlotExists(ctx context.Context, pool *pgxpool.Pool, slotName string) (string, bool, error) {
	var confirmedLSN *string
	err := pool.QueryRow(ctx, `
		SELECT confirmed_flush_lsn::text
		FROM pg_replication_slots
		WHERE slot_name = $1
	`, slotName).Scan(&confirmedLSN)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", false, nil
		}
		return "", false, err
	}

	if confirmedLSN == nil {
		return "", true, nil
	}
	return *confirmedLSN, true, nil
}

func resolveResumeSlot(ctx context.Context, pool *pgxpool.Pool, currentSlot, legacySlot string) (string, bool, bool, error) {
	if _, exists, err := checkSlotExists(ctx, pool, currentSlot); err != nil {
		return "", false, false, fmt.Errorf("failed to check replication slot %s: %w", currentSlot, err)
	} else if exists {
		return currentSlot, true, false, nil
	}
	if legacySlot == "" || legacySlot == currentSlot {
		return currentSlot, false, false, nil
	}

	var active bool
	err := pool.QueryRow(ctx, `
		SELECT active
		FROM pg_replication_slots
		WHERE slot_name = $1 AND slot_type = 'logical'
	`, legacySlot).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentSlot, false, false, nil
	}
	if err != nil {
		return "", false, false, fmt.Errorf("failed to inspect legacy replication slot %s: %w", legacySlot, err)
	}
	if active {
		return "", false, false, fmt.Errorf("legacy replication slot %s is active; stop the older connector before upgrading", legacySlot)
	}
	config.Debug("[CDC] Resuming with legacy automatic replication slot %s", legacySlot)
	return legacySlot, true, true, nil
}

// waitReplicationSlotReleased waits for PostgreSQL to mark a slot inactive
// after the previous replication connection was closed. Claiming a still-active
// slot makes StartReplication fail. This is best-effort with a bounded wait; if
// the slot stays active, the subsequent StartReplication returns the real error.
func waitReplicationSlotReleased(ctx context.Context, pool *pgxpool.Pool, slotName string) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		var active bool
		err := pool.QueryRow(ctx, "SELECT active FROM pg_replication_slots WHERE slot_name = $1", slotName).Scan(&active)
		if err != nil || !active {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// dropSlot drops the replication slot
func (s *Snapshot) dropSlot(ctx context.Context) error {
	_, err := s.source.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", s.slotName)
	return err
}

func (s *Snapshot) readWithSnapshot(ctx context.Context, snapshotName string, lsn pglogrepl.LSN, results chan<- source.RecordBatchResult, opts source.ReadOptions) error {
	conn, err := s.source.queryPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Start transaction with REPEATABLE READ isolation level (required for snapshot import)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Set transaction snapshot
	_, err = tx.Exec(ctx, fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshotName))
	if err != nil {
		return fmt.Errorf("failed to set transaction snapshot: %w", err)
	}

	config.Debug("[CDC] Reading snapshot data from %s", s.tableName)

	// Build select query (excluding CDC columns as they don't exist in source)
	sourceColumns := s.tableSchema.Columns[:sourceColumnCount(s.tableSchema)]
	colNames := make([]string, len(sourceColumns))
	for i, col := range sourceColumns {
		colNames[i] = quoteIdentifier(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTableName(s.tableName))
	config.Debug("[CDC] Snapshot query: %s", query)

	// A replacement snapshot can run the same SELECT text after DDL changed a
	// column OID. Describe it afresh so pgx cannot decode rows with the cached
	// pre-DDL statement description.
	rows, err := tx.Query(ctx, query, pgx.QueryExecModeDescribeExec)
	if err != nil {
		return fmt.Errorf("failed to query: %w", err)
	}
	defer func() { rows.Close() }()

	if opts.CDCSnapshotReplace {
		// A snapshot is a complete replacement boundary. Consumers that opt in
		// discard target rows left by an interrupted snapshot or a lost slot.
		if err := sendResult(ctx, results, source.RecordBatchResult{Truncate: true}); err != nil {
			return err
		}
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	lsnStr := FormatLSN(lsn)
	syncedAt := time.Now().UTC()

	// Build Arrow schema including CDC columns
	arrowSchema := buildArrowSchema(s.tableSchema.Columns)

	batchNum := 0
	var totalRows int64

	for {
		record, count, err := s.rowsToBatch(rows, arrowSchema, sourceColumns, batchSize, lsnStr, syncedAt)
		if err != nil {
			return fmt.Errorf("failed to convert rows to batch: %w", err)
		}

		if count == 0 {
			break
		}

		batchNum++
		totalRows += count
		config.Debug("[CDC] Snapshot batch %d: %d rows (total: %d)", batchNum, count, totalRows)

		if err := sendResult(ctx, results, source.RecordBatchResult{Batch: record}); err != nil {
			return err
		}
	}

	config.Debug("[CDC] Snapshot completed: %d rows in %d batches", totalRows, batchNum)
	return nil
}

func (s *Snapshot) rowsToBatch(rows pgx.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int, lsn string, syncedAt time.Time) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()

	// Create builders for all columns including CDC columns
	builders := make([]array.Builder, len(s.tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	var rowCount int64
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to get values: %w", err)
		}

		// Append source column values
		for i, val := range values {
			converted, err := convertValue(val, columns[i])
			if err != nil {
				for _, b := range builders {
					b.Release()
				}
				return nil, 0, fmt.Errorf("failed to convert snapshot column %q: %w", columns[i].Name, err)
			}
			arrowconv.AppendValue(builders[i], converted)
		}

		builders[len(columns)].(*array.StringBuilder).Append(lsn)
		builders[len(columns)+1].(*array.BooleanBuilder).Append(false)
		builders[len(columns)+2].(*array.TimestampBuilder).Append(arrow.Timestamp(syncedAt.UnixMicro()))
		builders[len(columns)+3].(*array.StringBuilder).Append("[]")

		rowCount++

		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	if rowCount == 0 {
		for _, b := range builders {
			b.Release()
		}
		if err := rows.Err(); err != nil {
			return nil, 0, fmt.Errorf("error iterating rows: %w", err)
		}
		return nil, 0, nil
	}

	if err := rows.Err(); err != nil {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}

func convertValue(val interface{}, col schema.Column) (interface{}, error) {
	if val == nil {
		return nil, nil
	}
	switch v := val.(type) {
	case pgtype.Numeric:
		if !v.Valid {
			return nil, nil
		}
		if v.NaN {
			return nil, fmt.Errorf("PostgreSQL numeric NaN is not representable as %s", col.DataType)
		}
		if v.InfinityModifier != pgtype.Finite {
			return nil, fmt.Errorf("PostgreSQL numeric %s is not representable as %s", v.InfinityModifier, col.DataType)
		}
		if v.Int == nil {
			return nil, fmt.Errorf("PostgreSQL numeric has no finite coefficient")
		}
		return numericToBigInt(v, col.Scale), nil
	case pgtype.InfinityModifier:
		return nil, fmt.Errorf("PostgreSQL %s is not representable as %s", v, col.DataType)
	case [16]byte:
		// pgx decodes uuid to [16]byte; the Arrow layer stores UUIDs as their
		// canonical string form (matching the WAL decode paths).
		if col.DataType == schema.TypeUUID {
			return formatUUID(v[:]), nil
		}
		return val, nil
	default:
		return val, nil
	}
}

func numericToBigInt(num pgtype.Numeric, targetScale int) *big.Int {
	result := new(big.Int).Set(num.Int)

	currentExp := int(num.Exp)
	targetExp := -targetScale

	diff := currentExp - targetExp
	if diff > 0 {
		multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(diff)), nil)
		result.Mul(result, multiplier)
	} else if diff < 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-diff)), nil)
		result.Div(result, divisor)
	}

	return result
}
