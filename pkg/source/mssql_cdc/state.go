package mssql_cdc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/bruin-data/ingestr/pkg/source"
)

var (
	_ source.TableExistenceChecker          = (*MSSQLCDCSource)(nil)
	_ source.TableIncarnationProvider       = (*MSSQLCDCSource)(nil)
	_ source.TableSchemaFingerprintProvider = (*MSSQLCDCSource)(nil)
	_ source.CDCStateProvider               = (*MSSQLCDCSource)(nil)
)

func (s *MSSQLCDCSource) resetCDCState() {
	s.stateMu.Lock()
	s.state = source.CDCStateCommitToken{SnapshotPositions: make(map[string]string)}
	s.stateMu.Unlock()
}

// recordSnapshotState marks table's full snapshot as included in all results
// emitted so far, at the given stored position.
func (s *MSSQLCDCSource) recordSnapshotState(table, position string) {
	if table == "" || position == "" {
		return
	}
	s.stateMu.Lock()
	if s.state.SnapshotPositions == nil {
		s.state.SnapshotPositions = make(map[string]string)
	}
	s.state.SnapshotPositions[table] = position
	s.stateMu.Unlock()
}

// recordCheckpointState advances the globally safe position: every change at
// or below it has been emitted for every table in the read.
func (s *MSSQLCDCSource) recordCheckpointState(position string) {
	if position == "" {
		return
	}
	s.stateMu.Lock()
	s.state.Position = position
	s.stateMu.Unlock()
}

// CDCState returns the state produced by the read so far. The pipeline
// persists it after a batch write succeeds; streaming flushes consume the
// same token incrementally via CommitToken markers.
func (s *MSSQLCDCSource) CDCState() source.CDCStateCommitToken {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	snapshots := make(map[string]string, len(s.state.SnapshotPositions))
	for table, position := range s.state.SnapshotPositions {
		snapshots[table] = position
	}
	return source.CDCStateCommitToken{
		Position:          s.state.Position,
		SnapshotPositions: snapshots,
	}
}

// emitStateToken sends a batchless result carrying the current cumulative
// state token. Only the streaming executor consumes CommitToken markers, so
// batch mode relies on CDCState() after the write instead.
func (s *MSSQLCDCSource) emitStateToken(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	if !opts.Streaming {
		return nil
	}
	return emitResult(ctx, results, source.RecordBatchResult{CommitToken: s.CDCState(), TableName: resultTable})
}

// watermarkStoredLSN encodes a fully delivered harvest watermark. It shares
// the complete-snapshot encoding: everything at the LSN is included, so a
// resume reads strictly after it with no boundary re-read.
func watermarkStoredLSN(lsn string) string {
	return snapshotCompleteLSN(lsn)
}

func (s *MSSQLCDCSource) TableExists(ctx context.Context, table string) (bool, error) {
	schemaName, tableName := parseTableName(table)
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT CAST(CASE WHEN EXISTS (
			SELECT 1
			FROM sys.tables AS t
			JOIN sys.schemas AS sc ON sc.schema_id = t.schema_id
			WHERE sc.name = @p1 AND t.name = @p2
		) THEN 1 ELSE 0 END AS bit)`, schemaName, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check source table existence for %s: %w", table, err)
	}
	return exists, nil
}

// TableIncarnation identifies one life of a source table. object_id alone can
// be reused after a drop, so the creation time is folded in: a dropped and
// recreated table always yields a new incarnation, invalidating resume state
// that was recorded against the old one.
func (s *MSSQLCDCSource) TableIncarnation(ctx context.Context, table string) (string, error) {
	schemaName, tableName := parseTableName(table)
	var incarnation string
	err := s.db.QueryRowContext(ctx, `
		SELECT CONVERT(varchar(20), t.object_id) + N':' + CONVERT(varchar(33), t.create_date, 126)
		FROM sys.tables AS t
		JOIN sys.schemas AS sc ON sc.schema_id = t.schema_id
		WHERE sc.name = @p1 AND t.name = @p2`, schemaName, tableName).Scan(&incarnation)
	if err != nil {
		return "", fmt.Errorf("failed to read source table incarnation for %s: %w", table, err)
	}
	return incarnation, nil
}

// TableSchemaFingerprint hashes the schema this source actually delivers: the
// capture instance's identity and captured columns, plus the source table's
// primary key. A recreated capture instance or a changed merge key therefore
// changes the fingerprint and invalidates recorded resume state.
//
// The configured capture_instance applies here because single-table reads
// honor it; multi-table reads ignore it and fingerprint their own selected
// metadata via fingerprintCaptureInstance.
func (s *MSSQLCDCSource) TableSchemaFingerprint(ctx context.Context, table string) (string, error) {
	meta, err := s.getTableMetadata(ctx, table, s.cdcConfig.CaptureInstance)
	if err != nil {
		return "", err
	}
	return s.fingerprintCaptureInstance(ctx, meta)
}

func (s *MSSQLCDCSource) fingerprintCaptureInstance(ctx context.Context, meta tableMetadata) (string, error) {
	h := sha256.New()
	writeFingerprintValues(h, meta.CaptureInstance)

	var startLSN string
	err := s.db.QueryRowContext(
		ctx,
		"SELECT CONVERT(varchar(20), start_lsn, 2) FROM cdc.change_tables WHERE capture_instance = @p1 AND end_lsn IS NULL",
		meta.CaptureInstance,
	).Scan(&startLSN)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint capture instance %s: %w", meta.CaptureInstance, err)
	}
	writeFingerprintValues(h, startLSN)

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.name, t.name, CONVERT(int, c.is_nullable), CONVERT(int, c.precision), CONVERT(int, c.scale), CONVERT(int, c.max_length)
		FROM sys.columns AS c
		JOIN sys.types AS t ON c.user_type_id = t.user_type_id
		WHERE c.object_id = OBJECT_ID(@p1)
		  AND c.name NOT LIKE '__$%'
		ORDER BY c.column_id`, meta.ChangeTable)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint captured columns for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	columnCount := 0
	for rows.Next() {
		var name, typeName string
		var nullable, precision, scale, maxLength int
		if err := rows.Scan(&name, &typeName, &nullable, &precision, &scale, &maxLength); err != nil {
			return "", fmt.Errorf("failed to scan captured-column fingerprint for %s: %w", tableName(meta), err)
		}
		writeFingerprintValues(h, name, typeName, fmt.Sprint(nullable), fmt.Sprint(precision), fmt.Sprint(scale), fmt.Sprint(maxLength))
		columnCount++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("failed to read captured-column fingerprint for %s: %w", tableName(meta), err)
	}
	if columnCount == 0 {
		return "", fmt.Errorf("capture instance %s has no captured columns", meta.CaptureInstance)
	}

	pks, err := s.primaryKeys(ctx, meta)
	if err != nil {
		return "", err
	}
	writeFingerprintValues(h, pks...)

	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeFingerprintValues(h hash.Hash, values ...string) {
	var length [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(value))
	}
}
