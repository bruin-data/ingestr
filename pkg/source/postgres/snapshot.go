package postgres

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var snapshotIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{8}-[1-9][0-9]*$`)

func parseSnapshotURI(uri string) (string, string, error) {
	normalizedURI := uri
	if strings.Contains(uri, "+") {
		parts := strings.SplitN(uri, "://", 2)
		if len(parts) == 2 {
			normalizedURI = "postgres://" + parts[1]
		}
	}
	parsed, err := url.Parse(normalizedURI)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse postgres source URI: %w", err)
	}
	params, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse postgres source parameters: %w", err)
	}
	values, exists := params["snapshot"]
	if !exists {
		return normalizedURI, "", nil
	}
	if len(values) != 1 || !snapshotIDPattern.MatchString(values[0]) {
		return "", "", fmt.Errorf("snapshot must be a single exported PostgreSQL snapshot ID, such as 00000003-0000001B-1")
	}
	params.Del("snapshot")
	parsed.RawQuery = params.Encode()
	return parsed.String(), values[0], nil
}

func (s *PostgresSource) acquireReadConnection(ctx context.Context) (*pgxpool.Conn, func(), error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire postgres connection: %w", err)
	}
	if s.snapshot == "" {
		return conn, conn.Release, nil
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf("failed to begin postgres snapshot transaction: %w", err)
	}
	release := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
		conn.Release()
	}
	// Snapshot IDs are validated before interpolation because SET does not accept bind parameters.
	if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+s.snapshot+"'"); err != nil {
		release()
		return nil, nil, fmt.Errorf("failed to import postgres snapshot %s; keep its exporting session open until ingestion finishes: %w", s.snapshot, err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		release()
		return nil, nil, fmt.Errorf("failed to disable row filtering for postgres snapshot: %w", err)
	}
	return conn, release, nil
}
