package postgres_cdc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
)

func (s *PostgresCDCSource) TableSchemaFingerprint(ctx context.Context, table string) (string, error) {
	if s.queryPool == nil {
		return "", fmt.Errorf("postgres CDC source is not connected")
	}
	schemaName, tableName := parseTableName(table)
	h := sha256.New()

	rows, err := s.queryPool.Query(ctx, `
		SELECT a.attnum, a.attname, a.atttypid::text, a.atttypmod,
		       a.attnotnull, a.attidentity::text, a.attgenerated::text,
		       a.attcollation::text,
		       COALESCE(pg_get_expr(d.adbin, d.adrelid), ''),
		       c.relreplident::text
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2
		  AND c.relkind IN ('r', 'p')
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum
	`, schemaName, tableName)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint schema for %s: %w", table, err)
	}
	defer rows.Close()

	columnCount := 0
	for rows.Next() {
		var (
			attnum                         int16
			name, typeOID, identity        string
			generated, collation, defaultV string
			replicaIdentity                string
			typeMod                        int32
			notNull                        bool
		)
		if err := rows.Scan(&attnum, &name, &typeOID, &typeMod, &notNull, &identity, &generated, &collation, &defaultV, &replicaIdentity); err != nil {
			return "", fmt.Errorf("failed to scan schema fingerprint for %s: %w", table, err)
		}
		writeFingerprintValues(
			h,
			fmt.Sprint(attnum), name, typeOID, fmt.Sprint(typeMod), fmt.Sprint(notNull),
			identity, generated, collation, defaultV, replicaIdentity,
		)
		columnCount++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("failed to read schema fingerprint for %s: %w", table, err)
	}
	rows.Close()
	if columnCount == 0 {
		return "", fmt.Errorf("table %s not found or has no columns", table)
	}

	indexRows, err := s.queryPool.Query(ctx, `
		SELECT i.indisprimary, i.indisreplident, i.indkey::text,
		       pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
		  AND (i.indisprimary OR i.indisreplident)
		ORDER BY i.indisprimary DESC, i.indisreplident DESC, i.indexrelid
	`, schemaName, tableName)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint table keys for %s: %w", table, err)
	}
	defer indexRows.Close()
	for indexRows.Next() {
		var primary, replicaIdentity bool
		var key, definition string
		if err := indexRows.Scan(&primary, &replicaIdentity, &key, &definition); err != nil {
			return "", fmt.Errorf("failed to scan table-key fingerprint for %s: %w", table, err)
		}
		writeFingerprintValues(h, fmt.Sprint(primary), fmt.Sprint(replicaIdentity), key, definition)
	}
	if err := indexRows.Err(); err != nil {
		return "", fmt.Errorf("failed to read table-key fingerprint for %s: %w", table, err)
	}
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
