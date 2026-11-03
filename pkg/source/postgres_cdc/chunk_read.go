package postgres_cdc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type snapshotChunk struct {
	Batch    arrow.RecordBatch
	LastKey  []string
	UpperKey []string
	Boundary pglogrepl.LSN
	Done     bool
}

func (s *PostgresCDCSource) readSnapshotChunk(ctx context.Context, table source.SourceTableInfo, slotName string, after, upper []string, size int, maxBytes int64, timeout time.Duration) (chunk snapshotChunk, retErr error) {
	if size <= 0 || maxBytes < 0 || timeout <= 0 {
		return chunk, fmt.Errorf("snapshot chunk size and timeout must be positive and byte limit must be nonnegative")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.requireSnapshotAllowed(); err != nil {
		return chunk, err
	}
	if err := validateSnapshotChunkBounds(table.PrimaryKeys, after, upper); err != nil {
		return chunk, err
	}
	if err := s.validateChunkedTable(ctx, table); err != nil {
		return chunk, err
	}
	defer func() {
		if retErr != nil && chunk.Batch != nil {
			chunk.Batch.Release()
			chunk.Batch = nil
		}
	}()

	replConn, err := s.openReplicationConn(ctx)
	if err != nil {
		return chunk, err
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := replConn.Close(cleanupCtx); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("failed to close snapshot chunk replication connection: %w", err))
		}
	}()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return chunk, fmt.Errorf("failed to name snapshot chunk slot: %w", err)
	}
	created, err := s.createLogicalSlot(ctx, replConn, truncateSlotName(slotName+"_chunk_"+hex.EncodeToString(suffix[:])), true)
	if err != nil {
		return chunk, fmt.Errorf("failed to create snapshot chunk slot: %w", err)
	}
	chunk.Boundary, err = pglogrepl.ParseLSN(created.ConsistentPoint)
	if err != nil {
		return chunk, fmt.Errorf("failed to parse snapshot chunk boundary: %w", err)
	}

	conn, err := s.queryPool.Acquire(ctx)
	if err != nil {
		return chunk, fmt.Errorf("failed to acquire snapshot chunk connection: %w", err)
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return chunk, fmt.Errorf("failed to begin snapshot chunk transaction: %w", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			retErr = errors.Join(retErr, fmt.Errorf("failed to roll back snapshot chunk transaction: %w", err))
		}
	}()
	if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+strings.ReplaceAll(created.SnapshotName, "'", "''")+"'"); err != nil {
		return chunk, fmt.Errorf("failed to import snapshot chunk snapshot: %w", err)
	}
	keyTypes, err := snapshotKeyTypes(ctx, tx, table.Name, table.PrimaryKeys)
	if err != nil {
		return chunk, err
	}
	chunk.UpperKey = slices.Clone(upper)
	if upper == nil {
		chunk.UpperKey, err = snapshotUpperKey(ctx, tx, table)
		if err != nil {
			return chunk, err
		}
		if chunk.UpperKey == nil {
			chunk.Done = true
			return chunk, nil
		}
	}

	columns := table.Schema.Columns[:sourceColumnCount(table.Schema)]
	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = quoteIdentifier(col.Name)
	}
	query, args := snapshotChunkQuery(columnNames, quoteTableName(table.Name), table.PrimaryKeys, keyTypes, after, chunk.UpperKey, size)
	description, err := tx.Conn().PgConn().Prepare(ctx, "", query, nil)
	if err != nil {
		return chunk, fmt.Errorf("failed to describe snapshot chunk query: %w", err)
	}
	for _, field := range description.Fields[:min(len(description.Fields), len(columns))] {
		if field.TableOID != 0 && fmt.Sprint(field.TableOID) != table.Incarnation {
			return chunk, fmt.Errorf("table %s was replaced during its chunked snapshot", table.Name)
		}
	}
	typeMap := tx.Conn().TypeMap()
	formats := postgres.TextResultFormats(typeMap, description.Fields, columns, snapshotTextColumn)
	for i := len(columns); i < len(formats); i++ {
		formats[i] = pgx.TextFormatCode
	}

	const cursorName = "__ingestr_snapshot_chunk"
	quotedCursor := quoteIdentifier(cursorName)
	if _, err := tx.Exec(ctx, "DECLARE "+quotedCursor+" NO SCROLL CURSOR WITHOUT HOLD FOR "+query, args...); err != nil {
		return chunk, fmt.Errorf("failed to declare snapshot chunk cursor: %w", err)
	}
	cursorOpen := true
	closeCursor := func(closeCtx context.Context) error {
		if !cursorOpen {
			return nil
		}
		if _, err := tx.Exec(closeCtx, "CLOSE "+quotedCursor); err != nil {
			return err
		}
		cursorOpen = false
		return nil
	}
	defer func() {
		if !cursorOpen {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := closeCursor(cleanupCtx); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("failed to close snapshot chunk cursor: %w", err))
		}
	}()

	snapshot := &Snapshot{tableSchema: table.Schema, typeMap: typeMap}
	builder := newSnapshotBatchBuilder(
		snapshot.tableSchema,
		buildArrowSchema(table.Schema.Columns),
		columns,
		len(table.PrimaryKeys),
		typeMap,
		FormatLSN(chunk.Boundary),
		time.Now().UTC(),
		maxBytes > 0,
	)
	defer builder.release()

	previousFetch := 0
	for builder.rowCount < int64(size) && (maxBytes == 0 || builder.accBytes < maxBytes) {
		remaining := size - int(builder.rowCount)
		budget := int64(0)
		if maxBytes > 0 {
			budget = maxBytes - builder.accBytes
		}
		fetchLimit := snapshotChunkFetchLimit(remaining, budget, builder.widestRow, previousFetch)
		rows := tx.Conn().PgConn().ExecParams(ctx, fmt.Sprintf("FETCH FORWARD %d FROM %s", fetchLimit, quotedCursor), nil, nil, nil, formats)
		fetched, err := appendSnapshotChunkRows(rows, builder)
		if err != nil {
			return chunk, err
		}
		if fetched == 0 {
			break
		}
		previousFetch = fetchLimit
	}

	if err := closeCursor(ctx); err != nil {
		return chunk, fmt.Errorf("failed to close snapshot chunk cursor: %w", err)
	}
	if builder.rowCount > 0 {
		chunk.Batch = builder.record()
		chunk.LastKey = builder.lastKey
	}
	chunk.Done = builder.rowCount == 0 || slices.Equal(chunk.LastKey, chunk.UpperKey)
	return chunk, nil
}

func snapshotChunkFetchLimit(remaining int, budget, widest int64, previous int) int {
	if remaining <= 0 {
		return 0
	}
	if budget == 0 {
		return remaining
	}
	if previous <= 0 {
		return 1
	}

	byBytes := int64(remaining)
	if widest > 0 {
		byBytes = (budget-1)/widest + 1
	}
	growth := remaining
	if previous < (remaining+31)/32 {
		growth = previous * 32
	}
	return min(remaining, int(byBytes), growth)
}

func appendSnapshotChunkRows(rows *pgconn.ResultReader, builder *snapshotBatchBuilder) (int, error) {
	count := 0
	for rows.NextRow() {
		if err := builder.appendRow(rows.Values(), rows.FieldDescriptions()); err != nil {
			_, _ = rows.Close()
			return 0, err
		}
		count++
	}
	if _, err := rows.Close(); err != nil {
		return 0, fmt.Errorf("error iterating snapshot chunk rows: %w", err)
	}
	return count, nil
}

func validateSnapshotChunkBounds(keys, after, upper []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("chunked snapshots require non-null unique keys")
	}
	if after != nil && len(after) != len(keys) || upper != nil && len(upper) != len(keys) {
		return fmt.Errorf("snapshot chunk bounds must contain one value per key column")
	}
	return nil
}

func snapshotChunkQuery(columns []string, table string, keys, keyTypes, after, upper []string, size int) (string, []any) {
	query, args := keyedSnapshotQuery(columns, table, keys, keyTypes, after)
	orderAt := strings.LastIndex(query, " ORDER BY ")
	order := query[orderAt:]
	query = query[:orderAt]
	quotedKeys, params := make([]string, len(keys)), make([]string, len(keys))
	for i, key := range keys {
		quotedKeys[i] = "__ingestr_snapshot." + quoteIdentifier(key)
		params[i] = fmt.Sprintf("$%d::text::%s", len(args)+i+1, keyTypes[i])
	}
	if after == nil {
		query += " WHERE "
	} else {
		query += " AND "
	}
	query += fmt.Sprintf("(%s) <= (%s)", strings.Join(quotedKeys, ", "), strings.Join(params, ", "))
	for _, value := range upper {
		args = append(args, value)
	}
	args = append(args, size)
	return query + order + fmt.Sprintf(" LIMIT $%d", len(args)), args
}

func snapshotUpperKey(ctx context.Context, tx pgx.Tx, table source.SourceTableInfo) ([]string, error) {
	keys, order := make([]string, len(table.PrimaryKeys)), make([]string, len(table.PrimaryKeys))
	for i, key := range table.PrimaryKeys {
		qualified := "__ingestr_snapshot." + quoteIdentifier(key)
		keys[i], order[i] = qualified+"::text", qualified+" DESC"
	}
	query := fmt.Sprintf("SELECT %s FROM %s AS __ingestr_snapshot ORDER BY %s LIMIT 1", strings.Join(keys, ", "), quoteTableName(table.Name), strings.Join(order, ", "))
	values := make([]string, len(keys))
	args := make([]any, len(keys))
	for i := range values {
		args[i] = &values[i]
	}
	if err := tx.QueryRow(ctx, query).Scan(args...); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read snapshot upper key for %s: %w", table.Name, err)
	}
	return values, nil
}

func (s *PostgresCDCSource) validateChunkedTable(ctx context.Context, table source.SourceTableInfo) error {
	if err := validateChunkedKeys(table); err != nil {
		return err
	}
	if table.Incarnation == "" || table.SchemaFingerprint == "" {
		return fmt.Errorf("chunked snapshot of %s requires its source identity and schema fingerprint", table.Name)
	}
	incarnation, err := s.TableIncarnation(ctx, table.Name)
	if err != nil {
		return err
	}
	fingerprint, err := s.TableSchemaFingerprint(ctx, table.Name)
	if err != nil {
		return err
	}
	if incarnation != table.Incarnation || fingerprint != table.SchemaFingerprint {
		return fmt.Errorf("source identity or schema changed during the chunked snapshot of %s", table.Name)
	}
	schemaName, tableName := parseTableName(table.Name)
	var identity string
	var indexed bool
	err = s.queryPool.QueryRow(ctx, `
		SELECT c.relreplident::text, EXISTS (
			SELECT 1 FROM pg_index i
			JOIN pg_class idx ON idx.oid = i.indexrelid
			JOIN pg_am am ON am.oid = idx.relam
			WHERE i.indrelid = c.oid AND i.indisunique AND i.indisvalid AND i.indisready
			  AND i.indpred IS NULL AND i.indexprs IS NULL AND am.amname = 'btree'
			  AND (SELECT array_agg(a.attname::text ORDER BY k.ord)
			       FROM unnest(i.indkey) WITH ORDINALITY k(attnum, ord)
			       JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
			       WHERE k.ord <= COALESCE((to_jsonb(i)->>'indnkeyatts')::int, i.indnatts)) = $3::text[]
			  AND NOT EXISTS (
				SELECT 1 FROM unnest(i.indkey) WITH ORDINALITY k(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
				JOIN pg_opclass op ON op.oid = i.indclass[k.ord::int - 1]
				WHERE k.ord <= COALESCE((to_jsonb(i)->>'indnkeyatts')::int, i.indnatts)
				  AND (NOT a.attnotnull OR NOT op.opcdefault OR i.indcollation[k.ord::int - 1] != a.attcollation)
			  )
		)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')`, schemaName, tableName, table.PrimaryKeys).Scan(&identity, &indexed)
	if err != nil {
		return fmt.Errorf("failed to validate chunked snapshot keys for %s: %w", table.Name, err)
	}
	if !indexed {
		return fmt.Errorf("chunked snapshot of %s requires a valid unique btree index on non-null keys %v in key order, using their default operators and collations", table.Name, table.PrimaryKeys)
	}
	if chunkedTableNeedsFullIdentity(table.Schema) && identity != "f" {
		return fmt.Errorf("chunked snapshot of %s requires REPLICA IDENTITY FULL for TOAST-capable columns; configure it before starting CDC", table.Name)
	}
	return nil
}

func validateChunkedKeys(table source.SourceTableInfo) error {
	if resumableSnapshotKeys(table) == nil {
		return fmt.Errorf("chunked snapshot of %s requires non-null unique keys", table.Name)
	}
	seen := make(map[string]bool, len(table.PrimaryKeys))
	for _, key := range table.PrimaryKeys {
		if seen[key] {
			return fmt.Errorf("chunked snapshot of %s has duplicate key column %q", table.Name, key)
		}
		seen[key] = true
	}
	return nil
}

func chunkedTableNeedsFullIdentity(tableSchema *schema.TableSchema) bool {
	for _, col := range tableSchema.Columns[:sourceColumnCount(tableSchema)] {
		switch col.DataType {
		case schema.TypeString, schema.TypeBinary, schema.TypeJSON, schema.TypeArray, schema.TypeDecimal, schema.TypeUnknown:
			return true
		}
	}
	return false
}
