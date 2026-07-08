package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// keepaliveInterval bounds how often we ping the walsender with a
// WALWritePosition-only standby status update during the destination-write
// phase. Postgres's default wal_sender_timeout is 60s and any send from the
// client resets it server-side; pinging every 5s leaves ample margin if the
// server is configured lower (e.g. 10s) without flooding the connection.
const keepaliveInterval = 5 * time.Second

// CDCMode represents the CDC operation mode
type CDCMode string

const (
	ModeBatch  CDCMode = "batch"  // Run once, exit when caught up
	ModeStream CDCMode = "stream" // Continuous streaming mode
)

// defaultPublicationName is the publication ingestr creates and manages when the
// URI does not specify one.
const defaultPublicationName = "ingestr_publication"

// defaultDiscoverInterval is how often a streaming run re-checks the source for
// tables that appeared after the stream started.
const defaultDiscoverInterval = 30 * time.Second

type CDCConfig struct {
	Publication   string
	SlotName      string
	Mode          CDCMode
	ResumeFromLSN string // If set, skip snapshot and resume streaming from this LSN
	DestSchema    string // If set, prepend this schema to destination table names (e.g. "dataset" for BigQuery)

	// DiscoverInterval is how often streaming mode checks for new tables on the
	// source. Zero disables mid-stream discovery.
	DiscoverInterval time.Duration
}

type PostgresCDCSource struct {
	queryPool *pgxpool.Pool  // Regular connection pool for queries
	replConn  *pgconn.PgConn // Replication connection
	uri       string
	// normalizedURI is the URI with the +cdc scheme suffix and CDC-specific query
	// params stripped; extra replication connections are derived from it.
	normalizedURI string
	// managedPublication is true when ingestr owns the publication (none was
	// supplied in the URI) and may reconcile its table set.
	managedPublication bool
	cdcConfig          CDCConfig
	// pos holds the LSN the pipeline has confirmed durable in streaming mode.
	// It is shared between the pipeline goroutine (CommitStream) and the
	// replication goroutine (standby status updates).
	pos *streamPosition
	// caughtUp holds the LSN a batch run streamed up to (its targetLSN). It is
	// sent as a final standby status update by FinalizeBatch so the slot's
	// confirmed_flush_lsn advances even when the run catches up before the
	// replicator's 10s standby timer fires. Stays zero in streaming mode.
	caughtUp *streamPosition

	// keepalive coordinates a goroutine that periodically pings the
	// walsender with a WALWritePosition-only standby update during the
	// destination-write phase. Without it, a write that outlasts
	// wal_sender_timeout causes PG to kill the walsender; the later
	// FinalizeBatch's SendStandbyStatusUpdate then succeeds at the TCP layer
	// but the slot's confirmed_flush_lsn never advances.
	keepaliveMu   sync.Mutex
	keepaliveStop chan struct{}
	keepaliveDone chan struct{}
}

func NewPostgresCDCSource() *PostgresCDCSource {
	return &PostgresCDCSource{pos: newStreamPosition(), caughtUp: newStreamPosition()}
}

func (s *PostgresCDCSource) Schemes() []string {
	return []string{"postgres+cdc", "postgresql+cdc"}
}

func (s *PostgresCDCSource) Connect(ctx context.Context, uri string) error {
	cdcConfig, normalizedURI, err := parseURIConfig(uri)
	if err != nil {
		return fmt.Errorf("failed to parse CDC config: %w", err)
	}

	// Create query pool for regular SQL operations
	pgConfig, err := pgxpool.ParseConfig(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	queryPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := queryPool.Ping(ctx); err != nil {
		queryPool.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	// When no publication is specified in the URI, ingestr manages a default
	// publication. Reconcile it on every run so it tracks the current set of
	// logged tables; unlogged tables cannot be replicated and are skipped.
	managedPublication := cdcConfig.Publication == ""
	if managedPublication {
		cdcConfig.Publication = defaultPublicationName
		if err := ensureManagedPublication(ctx, queryPool, cdcConfig.Publication); err != nil {
			queryPool.Close()
			return fmt.Errorf("failed to ensure publication: %w", err)
		}
	}

	// Create replication connection
	replConnStr := buildReplicationConnString(normalizedURI)
	replConn, err := pgconn.Connect(ctx, replConnStr)
	if err != nil {
		queryPool.Close()
		return fmt.Errorf("failed to create replication connection: %w", err)
	}

	s.queryPool = queryPool
	s.replConn = replConn
	s.uri = uri
	s.normalizedURI = normalizedURI
	s.managedPublication = managedPublication
	s.cdcConfig = cdcConfig

	return nil
}

// openReplicationConn opens an additional replication connection, e.g. for a
// temporary snapshot slot that must not disturb the main replication stream.
func (s *PostgresCDCSource) openReplicationConn(ctx context.Context) (*pgconn.PgConn, error) {
	conn, err := pgconn.Connect(ctx, buildReplicationConnString(s.normalizedURI))
	if err != nil {
		return nil, fmt.Errorf("failed to open replication connection: %w", err)
	}
	return conn, nil
}

// reconnectReplication replaces the main replication connection with a fresh
// one. Needed after a streaming rebuild: once StartReplication has put a
// connection into CopyBoth mode there is no clean way back, so a new
// StartReplication requires a new connection. The persistent slot is untouched.
func (s *PostgresCDCSource) reconnectReplication(ctx context.Context) error {
	if s.replConn != nil {
		_ = s.replConn.Close(ctx)
		s.replConn = nil
	}
	conn, err := s.openReplicationConn(ctx)
	if err != nil {
		return err
	}
	s.replConn = conn
	return nil
}

func (s *PostgresCDCSource) Close(ctx context.Context) error {
	var errs []error

	// Stop the keepalive goroutine first so it does not race the connection
	// close. stopKeepalive is idempotent.
	s.stopKeepalive()

	if s.replConn != nil {
		if err := s.replConn.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close replication connection: %w", err))
		}
	}

	if s.queryPool != nil {
		s.queryPool.Close()
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// startKeepalive spawns a goroutine that pings the walsender every
// keepaliveInterval with WALWritePosition=lsn (no WALFlush, so the slot does
// not advance from these pings). It is meant to be called once the readers
// have stopped pulling from the replication connection (after streamLoop
// returns) so the destination-write phase can run for longer than
// wal_sender_timeout without the walsender being killed server-side.
//
// Calling startKeepalive a second time without an intervening stop is a no-op.
// The returned goroutine exits on ctx cancellation, stopKeepalive, or the
// first standby-send error (a dead conn surfaces as an error here and the
// follow-up FinalizeBatch send will fail in the same way; the run still
// completes — the slot just stays where it was, same as before).
func (s *PostgresCDCSource) startKeepalive(ctx context.Context, lsn pglogrepl.LSN) {
	if s.replConn == nil || lsn == 0 {
		return
	}
	s.keepaliveMu.Lock()
	if s.keepaliveStop != nil {
		s.keepaliveMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	s.keepaliveStop = stop
	s.keepaliveDone = done
	s.keepaliveMu.Unlock()

	go func() {
		defer close(done)
		send := func() bool {
			err := pglogrepl.SendStandbyStatusUpdate(ctx, s.replConn, pglogrepl.StandbyStatusUpdate{
				WALWritePosition: lsn,
			})
			if err != nil {
				config.Debug("[CDC] Keepalive standby status failed (replication connection lost): %v", err)
				return false
			}
			config.Debug("[CDC] Keepalive standby status sent at LSN %s", lsn)
			return true
		}
		// Send once immediately so the walsender's idle timer resets at the
		// start of the destination-write phase. Without this, a
		// wal_sender_timeout shorter than keepaliveInterval would kill the
		// connection before the first tick.
		if !send() {
			return
		}
		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if !send() {
					return
				}
			}
		}
	}()
}

// stopKeepalive signals the keepalive goroutine to exit and waits for it.
// Safe to call multiple times.
func (s *PostgresCDCSource) stopKeepalive() {
	s.keepaliveMu.Lock()
	stop := s.keepaliveStop
	done := s.keepaliveDone
	s.keepaliveStop = nil
	s.keepaliveDone = nil
	s.keepaliveMu.Unlock()
	if stop == nil {
		return
	}
	select {
	case <-stop:
		// already closed
	default:
		close(stop)
	}
	if done != nil {
		<-done
	}
}

func (s *PostgresCDCSource) HandlesIncrementality() bool {
	return false
}

// SupportsStreaming reports that Postgres CDC can run in continuous mode.
func (s *PostgresCDCSource) SupportsStreaming() bool {
	return true
}

// DefaultStreamingStrategy returns merge: CDC changes (including deletes) must
// be applied by primary key.
func (s *PostgresCDCSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

// CommitStream records the durable LSN reported by the pipeline after a
// successful flush. The replication goroutine reads it when sending standby
// status updates so the slot's confirmed_flush_lsn only advances past durable
// data. The token is the pglogrepl.LSN attached to the last flushed batch.
func (s *PostgresCDCSource) CommitStream(_ context.Context, token any) error {
	lsn, ok := token.(pglogrepl.LSN)
	if !ok {
		return fmt.Errorf("postgres cdc: unexpected commit token type %T", token)
	}
	if s.pos == nil {
		return fmt.Errorf("postgres cdc: stream position not initialized")
	}
	s.pos.Commit(lsn)
	config.Debug("[CDC] Confirmed durable LSN: %s", lsn)
	return nil
}

// recordCaughtUpLSN records the LSN a batch run has streamed up to. It is sent
// to the slot by FinalizeBatch once the destination write is durable.
func (s *PostgresCDCSource) recordCaughtUpLSN(lsn pglogrepl.LSN) {
	if s.caughtUp != nil {
		s.caughtUp.Commit(lsn)
	}
}

// FinalizeBatch sends a final standby status update confirming the LSN the batch
// run caught up to. The pipeline calls it only after a successful, durable
// write, so confirming the streamed position cannot discard WAL the destination
// has not persisted. Without it, a batch run that catches up before the
// replicator's 10s standby timer fires never advances the slot's
// confirmed_flush_lsn, so lag and retained WAL grow without bound across runs.
// Best-effort: a failure here does not fail an otherwise-successful ingest.
func (s *PostgresCDCSource) FinalizeBatch(ctx context.Context) error {
	if s.replConn == nil || s.caughtUp == nil {
		return nil
	}
	// Stop the keepalive goroutine before sending so we are the sole writer
	// on the replication connection. The keepalive only sends
	// WALWritePosition (it cannot have advanced the slot); the final send
	// below carries WALFlush=lsn which actually moves confirmed_flush_lsn.
	s.stopKeepalive()
	lsn := s.caughtUp.Committed()
	if lsn == 0 {
		return nil
	}
	err := pglogrepl.SendStandbyStatusUpdate(ctx, s.replConn, pglogrepl.StandbyStatusUpdate{
		WALWritePosition: lsn,
		WALFlushPosition: lsn,
		WALApplyPosition: lsn,
	})
	if err != nil {
		config.Debug("[CDC] Failed to send final standby status at LSN %s: %v", lsn, err)
		return fmt.Errorf("failed to send final standby status: %w", err)
	}
	config.Debug("[CDC] Sent final standby status confirming LSN %s", lsn)
	return nil
}

func (s *PostgresCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	return NewCDCTable(s, req)
}

func parseURIConfig(uri string) (CDCConfig, string, error) {
	cfg := CDCConfig{
		Mode: ModeBatch,
	}

	// Remove scheme suffix (+cdc)
	normalizedURI := uri
	if strings.HasPrefix(uri, "postgres+cdc://") {
		normalizedURI = "postgres://" + strings.TrimPrefix(uri, "postgres+cdc://")
	} else if strings.HasPrefix(uri, "postgresql+cdc://") {
		normalizedURI = "postgresql://" + strings.TrimPrefix(uri, "postgresql+cdc://")
	}

	parsed, err := url.Parse(normalizedURI)
	if err != nil {
		return cfg, "", fmt.Errorf("failed to parse URI: %w", err)
	}

	query := parsed.Query()

	cfg.Publication = query.Get("publication")
	cfg.SlotName = query.Get("slot")
	cfg.DestSchema = query.Get("dest_schema")

	if mode := query.Get("mode"); mode != "" {
		switch mode {
		case "batch":
			cfg.Mode = ModeBatch
		case "stream":
			cfg.Mode = ModeStream
		default:
			return cfg, "", fmt.Errorf("invalid mode: %s (must be 'batch' or 'stream')", mode)
		}
	}

	cfg.DiscoverInterval = defaultDiscoverInterval
	if raw := query.Get("discover_interval"); raw != "" {
		switch raw {
		case "0", "off":
			cfg.DiscoverInterval = 0
		default:
			d, err := time.ParseDuration(raw)
			if err != nil || d < 0 {
				return cfg, "", fmt.Errorf("invalid discover_interval: %s (must be a duration like '30s', or '0'/'off' to disable)", raw)
			}
			cfg.DiscoverInterval = d
		}
	}

	// Remove CDC-specific params from connection string
	query.Del("publication")
	query.Del("slot")
	query.Del("mode")
	query.Del("dest_schema")
	query.Del("discover_interval")
	parsed.RawQuery = query.Encode()

	return cfg, parsed.String(), nil
}

// pgTableRef identifies a Postgres table by schema and name.
type pgTableRef struct {
	schema string
	name   string
}

// quoted returns the schema-qualified, safely quoted identifier for use in DDL.
func (t pgTableRef) quoted() string {
	return pgx.Identifier{t.schema, t.name}.Sanitize()
}

// display returns a human-readable schema.table name for log and warning output.
func (t pgTableRef) display() string {
	return t.schema + "." + t.name
}

// quotePublicationTables renders refs as a comma-separated, safely quoted list
// for a CREATE/ALTER PUBLICATION ... TABLE clause.
func quotePublicationTables(tables []pgTableRef) string {
	parts := make([]string, len(tables))
	for i, t := range tables {
		parts[i] = t.quoted()
	}
	return strings.Join(parts, ", ")
}

// replicaIdentityClause is a SQL predicate over alias c (pg_class) that is true
// when a table has a replica identity usable for publishing UPDATE/DELETE:
// REPLICA IDENTITY FULL, DEFAULT backed by a primary key, or a valid USING INDEX.
// A table failing this would raise SQLSTATE 55000 on UPDATE/DELETE once it is
// part of a publication that publishes those operations.
const replicaIdentityClause = `(
	c.relreplident = 'f'
	OR (c.relreplident = 'd' AND EXISTS (
		SELECT 1 FROM pg_index i WHERE i.indrelid = c.oid AND i.indisprimary))
	OR (c.relreplident = 'i' AND EXISTS (
		SELECT 1 FROM pg_index i WHERE i.indrelid = c.oid AND i.indisreplident AND i.indisvalid))
)`

// skipReason explains why a table is excluded from the managed publication.
type skipReason int

const (
	skipUnlogged skipReason = iota
	skipNoReplicaIdentity
)

// skippedTable pairs an excluded table with the reason it was excluded.
type skippedTable struct {
	ref    pgTableRef
	reason skipReason
}

// warning returns the user-facing message describing why the table is skipped.
func (s skippedTable) warning(pubName string) string {
	switch s.reason {
	case skipUnlogged:
		return fmt.Sprintf("table %s is unlogged and will be skipped from CDC publication %q; changes to unlogged tables are not replicated", s.ref.display(), pubName)
	case skipNoReplicaIdentity:
		return fmt.Sprintf("table %s has no replica identity (no primary key) and will be skipped from CDC publication %q; including it would make UPDATE/DELETE on the source fail", s.ref.display(), pubName)
	default:
		return fmt.Sprintf("table %s will be skipped from CDC publication %q", s.ref.display(), pubName)
	}
}

// selectPublishableTables inspects the user tables and splits them into those
// that can be added to a logical-replication publication (logged, with a usable
// replica identity) and those that must be skipped, along with the reason. Only
// relations that physically hold rows are considered (relkind 'r' — ordinary
// tables and leaf partitions); partitioned parents are excluded so the
// publication never lists a parent alongside a partition it already covers,
// which Postgres rejects. Temporary tables and system catalogs are ignored.
func selectPublishableTables(ctx context.Context, pool *pgxpool.Pool) (included []pgTableRef, skipped []skippedTable, err error) {
	const q = `
		SELECT n.nspname, c.relname, c.relpersistence::text, ` + replicaIdentityClause + ` AS has_replica_identity
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY n.nspname, c.relname
	`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			t                  pgTableRef
			persistence        string
			hasReplicaIdentity bool
		)
		if err := rows.Scan(&t.schema, &t.name, &persistence, &hasReplicaIdentity); err != nil {
			return nil, nil, fmt.Errorf("failed to scan table: %w", err)
		}
		switch {
		case persistence == "u":
			skipped = append(skipped, skippedTable{ref: t, reason: skipUnlogged})
		case persistence != "p":
			// Temporary or other non-permanent relations are not replicated.
			continue
		case !hasReplicaIdentity:
			skipped = append(skipped, skippedTable{ref: t, reason: skipNoReplicaIdentity})
		default:
			included = append(included, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating tables: %w", err)
	}
	return included, skipped, nil
}

// ensureManagedPublication creates or updates the ingestr-managed publication so
// it covers exactly the tables currently eligible for logical replication. It
// runs on every connection (only when no publication is supplied in the URI), so
// the publication tracks tables added, dropped, or changed since the previous
// run. Tables that are unlogged or lack a usable replica identity are skipped
// with a warning.
func ensureManagedPublication(ctx context.Context, pool *pgxpool.Pool, pubName string) error {
	included, skipped, err := selectPublishableTables(ctx, pool)
	if err != nil {
		return err
	}

	for _, s := range skipped {
		fmt.Printf("Warning: %s\n", s.warning(pubName))
	}

	if len(included) == 0 {
		return fmt.Errorf("no tables eligible for replication for publication %q", pubName)
	}

	tableList := quotePublicationTables(included)
	// Use pgx.Identifier to safely quote identifiers - DDL does not support parameter placeholders.
	pubIdent := pgx.Identifier{pubName}.Sanitize()

	var pubAllTables bool
	err = pool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", pubName).Scan(&pubAllTables)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		config.Debug("[CDC] Creating publication %s for %d table(s)", pubName, len(included))
		if _, err = pool.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", pubIdent, tableList)); err != nil {
			// Another instance may have created it concurrently; fall back to
			// reconciling its table set instead of failing the run.
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && (pgErr.Code == pgErrDuplicate || pgErr.Code == pgErrAlreadyExists) {
				config.Debug("[CDC] Publication %s created concurrently; updating its table set", pubName)
				return setPublicationTables(ctx, pool, pubIdent, tableList)
			}
			return fmt.Errorf("failed to create publication: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("failed to check publication existence: %w", err)
	case pubAllTables:
		// A publication left over from an older ingestr as FOR ALL TABLES cannot
		// have its table set altered, so recreate it with the explicit list.
		config.Debug("[CDC] Recreating FOR ALL TABLES publication %s with explicit logged-table list", pubName)
		if _, err = pool.Exec(ctx, fmt.Sprintf("DROP PUBLICATION %s", pubIdent)); err != nil {
			return fmt.Errorf("failed to drop legacy publication: %w", err)
		}
		if _, err = pool.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", pubIdent, tableList)); err != nil {
			return fmt.Errorf("failed to recreate publication: %w", err)
		}
		return nil
	default:
		config.Debug("[CDC] Updating publication %s to cover %d table(s)", pubName, len(included))
		return setPublicationTables(ctx, pool, pubIdent, tableList)
	}
}

// setPublicationTables replaces the table set of an existing FOR TABLE publication.
func setPublicationTables(ctx context.Context, pool *pgxpool.Pool, pubIdent, tableList string) error {
	if _, err := pool.Exec(ctx, fmt.Sprintf("ALTER PUBLICATION %s SET TABLE %s", pubIdent, tableList)); err != nil {
		return fmt.Errorf("failed to update publication tables: %w", err)
	}
	return nil
}

func buildReplicationConnString(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return uri + "?replication=database"
	}

	query := parsed.Query()
	query.Set("replication", "database")
	parsed.RawQuery = query.Encode()

	config.Debug("[CDC] Replication connection string: %s", parsed.String())

	return parsed.String()
}

// MultiTableSource interface implementation

// IsMultiTable returns true if this CDC source should operate in multi-table mode.
// Returns true when no specific table is requested (source-table flag not provided).
func (s *PostgresCDCSource) IsMultiTable() bool {
	// CDC sources are multi-table by default - they capture changes from all tables in the publication
	return true
}

// GetTables returns all tables in the publication with their schemas.
func (s *PostgresCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	// Check if this is a "FOR ALL TABLES" publication
	var pubAllTables bool
	err := s.queryPool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", s.cdcConfig.Publication).Scan(&pubAllTables)
	if err != nil {
		return nil, fmt.Errorf("failed to check publication type: %w", err)
	}

	config.Debug("[CDC] Publication %s puballtables=%v", s.cdcConfig.Publication, pubAllTables)

	var query string
	if pubAllTables {
		// For "FOR ALL TABLES" publications, query all user tables directly.
		// Exclude unlogged tables (their changes never reach the WAL) and tables
		// without a usable replica identity (publishing them would make
		// UPDATE/DELETE on the source fail), mirroring the managed-publication path.
		query = `
			SELECT n.nspname, c.relname
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind = 'r'
			  AND c.relpersistence = 'p'
			  AND ` + replicaIdentityClause + `
			  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
			ORDER BY n.nspname, c.relname
		`
	} else {
		// For specific table publications, use pg_publication_tables
		query = `
			SELECT schemaname, tablename
			FROM pg_publication_tables
			WHERE pubname = $1
			ORDER BY schemaname, tablename
		`
	}

	var rows pgx.Rows
	if pubAllTables {
		rows, err = s.queryPool.Query(ctx, query)
	} else {
		rows, err = s.queryPool.Query(ctx, query, s.cdcConfig.Publication)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query publication tables: %w", err)
	}
	defer func() { rows.Close() }()

	var tables []source.SourceTableInfo
	for rows.Next() {
		var schemaName, tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		fullName := publicationTableFullName(schemaName, tableName)

		// Get schema for this table
		tableSchema, err := getTableSchema(ctx, s.queryPool, fullName)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for table %s: %w", fullName, err)
		}

		// Add CDC metadata columns
		tableSchema = addCDCColumns(tableSchema)

		tables = append(tables, source.SourceTableInfo{
			Name:        fullName,
			Schema:      tableSchema,
			PrimaryKeys: tableSchema.PrimaryKeys,
			DestSchema:  s.cdcConfig.DestSchema,
		})

		config.Debug("[CDC] Found table in publication: %s (PKs: %v)", fullName, tableSchema.PrimaryKeys)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(tables) == 0 {
		config.Debug("[CDC] No tables found. pubAllTables=%v, query used: %s", pubAllTables, query)
		return nil, fmt.Errorf("no tables found in publication %s", s.cdcConfig.Publication)
	}

	config.Debug("[CDC] Found %d tables in publication %s", len(tables), s.cdcConfig.Publication)
	return tables, nil
}

// publicationTableFullName renders a table name the way GetTables and the WAL
// decoder key tables: bare name for public, schema-qualified otherwise.
func publicationTableFullName(schemaName, tableName string) string {
	if schemaName == "public" {
		return tableName
	}
	return schemaName + "." + tableName
}

// listEligibleTableNames returns the names of the tables the CDC stream should
// currently cover, without fetching schemas. For the ingestr-managed
// publication this is the set of publishable tables (the publication is
// reconciled to it); for a user-supplied publication it is the publication's
// membership (or all eligible tables for FOR ALL TABLES publications).
func (s *PostgresCDCSource) listEligibleTableNames(ctx context.Context) (map[string]struct{}, error) {
	names := make(map[string]struct{})

	if s.managedPublication {
		included, _, err := selectPublishableTables(ctx, s.queryPool)
		if err != nil {
			return nil, err
		}
		for _, t := range included {
			names[publicationTableFullName(t.schema, t.name)] = struct{}{}
		}
		return names, nil
	}

	var pubAllTables bool
	err := s.queryPool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", s.cdcConfig.Publication).Scan(&pubAllTables)
	if err != nil {
		return nil, fmt.Errorf("failed to check publication type: %w", err)
	}

	var rows pgx.Rows
	if pubAllTables {
		rows, err = s.queryPool.Query(ctx, `
			SELECT n.nspname, c.relname
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind = 'r'
			  AND c.relpersistence = 'p'
			  AND `+replicaIdentityClause+`
			  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		`)
	} else {
		rows, err = s.queryPool.Query(ctx, `
			SELECT schemaname, tablename
			FROM pg_publication_tables
			WHERE pubname = $1
		`, s.cdcConfig.Publication)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list publication tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		names[publicationTableFullName(schemaName, tableName)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table names: %w", err)
	}
	return names, nil
}

// reconcilePublication refreshes the managed publication's table set so tables
// created after the stream started become part of it. No-op for user-supplied
// publications, whose membership the user controls.
func (s *PostgresCDCSource) reconcilePublication(ctx context.Context) error {
	if !s.managedPublication {
		return nil
	}
	return ensureManagedPublication(ctx, s.queryPool, s.cdcConfig.Publication)
}

// ReadAll reads CDC changes from all tables in the publication.
func (s *PostgresCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	tables, err := s.GetTables(ctx)
	if err != nil {
		return nil, err
	}

	reader := NewMultiTableCDCReader(s, tables, s.cdcConfig, opts.CDCResumeLSNs, opts.CDCSlotSuffix)
	return reader.Read(ctx, opts)
}

const (
	pgErrAlreadyExists = "42710"
	pgErrDuplicate     = "23505"
)

var (
	_ source.Source            = (*PostgresCDCSource)(nil)
	_ source.MultiTableSource  = (*PostgresCDCSource)(nil)
	_ source.StreamingSource   = (*PostgresCDCSource)(nil)
	_ source.StreamCommitter   = (*PostgresCDCSource)(nil)
	_ source.CDCBatchFinalizer = (*PostgresCDCSource)(nil)
)
