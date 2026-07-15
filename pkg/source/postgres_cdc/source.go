package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
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

// keepaliveInterval bounds how often we ping the walsender with a standby
// status update during the destination-write
// phase. Postgres's default wal_sender_timeout is 60s and any send from the
// client resets it server-side; pinging every 5s leaves ample margin if the
// server is configured lower (e.g. 10s) without flooding the connection.
const keepaliveInterval = 5 * time.Second

const finalizeConfirmationTimeout = 5 * time.Second

// defaultPublicationName is the publication ingestr creates and manages when the
// URI does not specify one.
const defaultPublicationName = "ingestr_publication"

// defaultDiscoverInterval is how often a streaming run re-checks the source for
// tables that appeared after the stream started.
const defaultDiscoverInterval = 30 * time.Second

type CDCConfig struct {
	Publication   string
	SlotName      string
	ResumeFromLSN string // If set, skip snapshot and resume streaming from this LSN
	DestSchema    string // If set, prepend this schema to destination table names (e.g. "dataset" for BigQuery)
	StateID       string // Optional explicit identity for destination-managed CDC state

	// DiscoverInterval is how often streaming mode checks for new tables on the
	// source. Zero disables mid-stream discovery.
	DiscoverInterval time.Duration

	// Binary opts into pgoutput's `binary 'true'` option (PostgreSQL 14+):
	// the server sends column values in binary send format, skipping the
	// text encode/parse round-trip for most types. Off by default because
	// only the standard scalar/array types have a binary decoder; exotic
	// column types fail fast with a descriptive error when enabled.
	Binary bool
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
	// serverVersion is the source's server_version_num, used to gate pgoutput
	// options added in newer PostgreSQL releases.
	serverVersion int
	// pos holds the LSN the pipeline has confirmed durable in streaming mode.
	// It is shared between the pipeline goroutine (CommitStream) and the
	// replication goroutine (standby status updates).
	pos *streamPosition
	// caughtUp holds the position reached by a batch run. FinalizeBatch confirms
	// it to the slot only when it came from an active replication stream;
	// snapshot-only runs keep it solely as a destination checkpoint marker.
	caughtUp *streamPosition

	// lag tracks the server's WAL head for replication-lag reporting.
	lag *lagState

	// keepalive coordinates a goroutine that periodically pings the
	// walsender with a WALWritePosition-only standby update during the
	// destination-write phase. Without it, a write that outlasts
	// wal_sender_timeout causes PG to kill the walsender; the later
	// FinalizeBatch's SendStandbyStatusUpdate then succeeds at the TCP layer
	// but the slot's confirmed_flush_lsn never advances.
	keepaliveMu     sync.Mutex
	keepaliveCancel context.CancelFunc
	keepaliveDone   chan struct{}

	// keylessWarned dedupes the append-only notice per table: GetTables runs
	// more than once per run (pipeline setup, ReadAll, stream rebuilds).
	keylessWarnedMu sync.Mutex
	keylessWarned   map[string]bool

	stateMu              sync.Mutex
	snapshotPositions    map[string]string
	snapshotIncarnations map[string]string
	snapshotSchemas      map[string]string
	caughtUpSlot         string
	caughtUpFromStream   bool

	connectorLeaseMu   sync.Mutex
	connectorLease     *postgresCDCLease
	connectorPreparing bool
	connectorIdentity  source.ConnectorIdentity
	legacySlots        map[string]bool
}

func NewPostgresCDCSource() *PostgresCDCSource {
	return &PostgresCDCSource{
		pos:                  newStreamPosition(),
		caughtUp:             newStreamPosition(),
		lag:                  newLagState(),
		snapshotPositions:    make(map[string]string),
		snapshotIncarnations: make(map[string]string),
		snapshotSchemas:      make(map[string]string),
	}
}

var (
	_ source.LagReporter                    = (*PostgresCDCSource)(nil)
	_ source.CDCStateProvider               = (*PostgresCDCSource)(nil)
	_ source.ConnectorLeaser                = (*PostgresCDCSource)(nil)
	_ source.ConnectorIdentityProvider      = (*PostgresCDCSource)(nil)
	_ source.ConnectorPreflightValidator    = (*PostgresCDCSource)(nil)
	_ source.ConnectorPreparer              = (*PostgresCDCSource)(nil)
	_ source.TableExistenceChecker          = (*PostgresCDCSource)(nil)
	_ source.TableSchemaFingerprintProvider = (*PostgresCDCSource)(nil)
	_ source.CDCLegacySlotFinalizer         = (*PostgresCDCSource)(nil)
)

func (s *PostgresCDCSource) ValidateConnectorPreflight(ctx context.Context, opts source.ConnectorPreflightOptions) error {
	if !opts.Streaming {
		if err := validateBatchBarrierSupport(s.serverVersion); err != nil {
			return err
		}
	}
	return s.validatePublicationShape(ctx)
}

func (s *PostgresCDCSource) validatePublicationShape(ctx context.Context) error {
	if s.managedPublication || s.serverVersion < 150000 {
		return nil
	}
	rows, err := s.queryPool.Query(ctx, `
		SELECT n.nspname, c.relname,
		       pr.prqual IS NOT NULL AS has_row_filter,
		       pr.prattrs IS NOT NULL AS has_column_list
		FROM pg_publication_rel pr
		JOIN pg_publication p ON p.oid = pr.prpubid
		JOIN pg_class c ON c.oid = pr.prrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE p.pubname = $1 AND (pr.prqual IS NOT NULL OR pr.prattrs IS NOT NULL)
		ORDER BY n.nspname, c.relname
	`, s.cdcConfig.Publication)
	if err != nil {
		return fmt.Errorf("failed to validate PostgreSQL publication %q: %w", s.cdcConfig.Publication, err)
	}
	defer rows.Close()
	var restricted []string
	for rows.Next() {
		var schemaName, tableName string
		var rowFilter, columnList bool
		if err := rows.Scan(&schemaName, &tableName, &rowFilter, &columnList); err != nil {
			return fmt.Errorf("failed to inspect PostgreSQL publication %q: %w", s.cdcConfig.Publication, err)
		}
		features := make([]string, 0, 2)
		if rowFilter {
			features = append(features, "row filter")
		}
		if columnList {
			features = append(features, "column list")
		}
		restricted = append(restricted, fmt.Sprintf("%s.%s (%s)", schemaName, tableName, strings.Join(features, ", ")))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to inspect PostgreSQL publication %q: %w", s.cdcConfig.Publication, err)
	}
	if len(restricted) > 0 {
		return fmt.Errorf("PostgreSQL publication %q uses row filters or column lists on %s; postgres+cdc requires full-row publications so the initial snapshot and WAL stream have identical row and column coverage", s.cdcConfig.Publication, strings.Join(restricted, "; "))
	}
	return nil
}

func (s *PostgresCDCSource) Schemes() []string {
	return []string{"postgres+cdc", "postgresql+cdc"}
}

func (s *PostgresCDCSource) Connect(ctx context.Context, uri string) error {
	cdcConfig, normalizedURI, err := parseURIConfig(uri)
	if err != nil {
		return fmt.Errorf("failed to parse CDC config: %w", err)
	}
	s.stateMu.Lock()
	s.snapshotPositions = make(map[string]string)
	s.snapshotIncarnations = make(map[string]string)
	s.snapshotSchemas = make(map[string]string)
	s.caughtUp = newStreamPosition()
	s.caughtUpSlot = ""
	s.caughtUpFromStream = false
	s.stateMu.Unlock()

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

	managedPublication := cdcConfig.Publication == ""
	if managedPublication {
		cdcConfig.Publication = defaultPublicationName
	}

	// Server version gates the pgoutput options added in PostgreSQL 14
	// (protocol v2 streaming, binary tuple format).
	var serverVersion int
	var database string
	if err := queryPool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int, current_database()").Scan(&serverVersion, &database); err != nil {
		queryPool.Close()
		return fmt.Errorf("failed to determine PostgreSQL server identity: %w", err)
	}
	if cdcConfig.Binary && serverVersion < 140000 {
		queryPool.Close()
		return fmt.Errorf("the binary=true option requires PostgreSQL 14 or newer (server reports %d)", serverVersion)
	}

	// Create replication connection
	replConnStr := buildReplicationConnString(normalizedURI)
	replConn, err := pgconn.Connect(ctx, replConnStr)
	if err != nil {
		queryPool.Close()
		return fmt.Errorf("failed to create replication connection: %w", err)
	}
	system, err := pglogrepl.IdentifySystem(ctx, replConn)
	if err != nil {
		_ = replConn.Close(ctx)
		queryPool.Close()
		return fmt.Errorf("failed to identify PostgreSQL server: %w", err)
	}

	s.queryPool = queryPool
	s.replConn = replConn
	s.uri = uri
	s.normalizedURI = normalizedURI
	s.managedPublication = managedPublication
	s.cdcConfig = cdcConfig
	s.serverVersion = serverVersion
	s.connectorIdentity = resolvedConnectorIdentity(system.SystemID, database, cdcConfig)

	return nil
}

func (s *PostgresCDCSource) ConnectorIdentity(_ context.Context) (source.ConnectorIdentity, error) {
	if s.queryPool == nil || s.connectorIdentity.Database == "" || s.connectorIdentity.Connector == "" {
		return source.ConnectorIdentity{}, fmt.Errorf("postgres CDC source is not connected")
	}
	return s.connectorIdentity, nil
}

func (s *PostgresCDCSource) TableIncarnation(ctx context.Context, table string) (string, error) {
	if s.queryPool == nil {
		return "", fmt.Errorf("postgres CDC source is not connected")
	}
	schemaName, tableName := parseTableName(table)
	var oid string
	err := s.queryPool.QueryRow(ctx, `
		SELECT c.oid::text
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')
	`, schemaName, tableName).Scan(&oid)
	if err != nil {
		return "", fmt.Errorf("failed to read source table incarnation for %s: %w", table, err)
	}
	return oid, nil
}

func (s *PostgresCDCSource) PrepareConnector(ctx context.Context) error {
	if !s.managedPublication {
		return nil
	}
	s.connectorLeaseMu.Lock()
	switch {
	case s.connectorLease != nil:
		s.connectorLeaseMu.Unlock()
		return fmt.Errorf("cannot prepare managed PostgreSQL publication %q while this source holds a connector lease; prepare the connector before acquiring its lease", s.cdcConfig.Publication)
	case s.connectorPreparing:
		s.connectorLeaseMu.Unlock()
		return fmt.Errorf("managed PostgreSQL publication %q preparation is already in progress", s.cdcConfig.Publication)
	default:
		s.connectorPreparing = true
		s.connectorLeaseMu.Unlock()
	}
	defer func() {
		s.connectorLeaseMu.Lock()
		s.connectorPreparing = false
		s.connectorLeaseMu.Unlock()
	}()
	return s.reconcileManagedPublication(ctx)
}

func (s *PostgresCDCSource) reconcileManagedPublication(ctx context.Context) error {
	lockConn, err := pgx.ConnectConfig(ctx, s.queryPool.Config().ConnConfig.Copy())
	if err != nil {
		return fmt.Errorf("failed to open PostgreSQL session for publication preparation: %w", err)
	}
	defer func() { _ = lockConn.Close(context.Background()) }()

	key := publicationLeaseKey(s.connectorIdentity.Database, s.cdcConfig.Publication)
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		return fmt.Errorf("failed to acquire managed publication lock: %w", err)
	}
	migrationKey := publicationMigrationLeaseKey(s.connectorIdentity.Database, s.cdcConfig.Publication)
	lockMigration := func() error {
		if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationKey); err != nil {
			return fmt.Errorf("failed to acquire managed publication migration lock: %w", err)
		}
		return nil
	}
	if err := ensureManagedPublication(ctx, s.queryPool, s.cdcConfig.Publication, lockMigration); err != nil {
		return fmt.Errorf("failed to ensure publication: %w", err)
	}
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
		s.replConn = nil
	}

	s.connectorLeaseMu.Lock()
	lease := s.connectorLease
	s.connectorLeaseMu.Unlock()
	if lease != nil {
		if err := lease.Release(); err != nil {
			errs = append(errs, fmt.Errorf("failed to release connector lease: %w", err))
		}
	}

	if s.queryPool != nil {
		s.queryPool.Close()
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// startKeepalive spawns a goroutine that drains the replication connection and
// pings the walsender every keepaliveInterval with WALWritePosition=lsn (no
// WALFlush, so the slot does not advance from these pings). It is meant to be
// called once the readers have stopped pulling from the replication connection
// so the destination-write phase can outlast wal_sender_timeout without either
// side of the CopyBoth stream blocking.
//
// Calling startKeepalive a second time without an intervening stop is a no-op.
// Scoped reader cancellation is intentionally ignored: the goroutine exits
// when FinalizeBatch or Close calls stopKeepalive, or on the first protocol
// error. This keeps the connection alive across the handoff from strategy
// execution to batch finalization.
func (s *PostgresCDCSource) startKeepalive(ctx context.Context, caughtUp, durable pglogrepl.LSN) {
	if s.replConn == nil || caughtUp == 0 {
		return
	}
	s.keepaliveMu.Lock()
	if s.keepaliveCancel != nil {
		s.keepaliveMu.Unlock()
		return
	}
	keepaliveCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	s.keepaliveCancel = cancel
	s.keepaliveDone = done
	s.keepaliveMu.Unlock()

	go func() {
		defer close(done)
		send := func() bool {
			status := standbyUpdate(false, caughtUp, 0, durable)
			err := sendStandbyStatusUpdate(keepaliveCtx, s.replConn, status)
			if err != nil {
				if keepaliveCtx.Err() != nil {
					return false
				}
				config.Debug("[CDC] Keepalive standby status failed (replication connection lost): %v", err)
				return false
			}
			config.Debug("[CDC] Keepalive standby status sent at LSN %s (durable %s)", caughtUp, durable)
			return true
		}
		// Send once immediately so the walsender's idle timer resets at the
		// start of the destination-write phase. Without this, a
		// wal_sender_timeout shorter than keepaliveInterval would kill the
		// connection before the first tick.
		if !send() {
			return
		}
		nextStatus := time.Now().Add(keepaliveInterval)
		for {
			if keepaliveCtx.Err() != nil {
				return
			}
			untilStatus := time.Until(nextStatus)
			if untilStatus <= 0 {
				if !send() {
					return
				}
				nextStatus = time.Now().Add(keepaliveInterval)
				continue
			}
			receiveCtx, receiveCancel := context.WithTimeout(keepaliveCtx, untilStatus)
			_, err := s.replConn.ReceiveMessage(receiveCtx)
			receiveTimedOut := errors.Is(receiveCtx.Err(), context.DeadlineExceeded)
			receiveCancel()
			if keepaliveCtx.Err() != nil {
				return
			}
			if err != nil && !receiveTimedOut {
				config.Debug("[CDC] Keepalive replication drain failed: %v", err)
				return
			}
		}
	}()
}

// stopKeepalive signals the keepalive goroutine to exit and waits for it.
// Safe to call multiple times.
func (s *PostgresCDCSource) stopKeepalive() {
	s.keepaliveMu.Lock()
	cancel := s.keepaliveCancel
	done := s.keepaliveDone
	s.keepaliveCancel = nil
	s.keepaliveDone = nil
	s.keepaliveMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
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
func (s *PostgresCDCSource) CommitStream(ctx context.Context, token any) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if stateToken, ok := token.(source.CDCStateCommitToken); ok {
		token = stateToken.SourceCommitToken
		if token == nil {
			return nil
		}
	}
	lsn, ok := token.(pglogrepl.LSN)
	if !ok {
		return fmt.Errorf("postgres cdc: unexpected commit token type %T", token)
	}
	if s.pos == nil {
		return fmt.Errorf("postgres cdc: stream position not initialized")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	s.pos.Commit(lsn)
	config.Debug("[CDC] Confirmed durable LSN: %s", lsn)
	return nil
}

func (s *PostgresCDCSource) recordSnapshotState(table string, lsn pglogrepl.LSN, incarnation, schemaFingerprint string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.snapshotPositions == nil {
		s.snapshotPositions = make(map[string]string)
	}
	s.snapshotPositions[table] = FormatLSN(lsn)
	if incarnation != "" {
		if s.snapshotIncarnations == nil {
			s.snapshotIncarnations = make(map[string]string)
		}
		s.snapshotIncarnations[table] = incarnation
	}
	if schemaFingerprint != "" {
		if s.snapshotSchemas == nil {
			s.snapshotSchemas = make(map[string]string)
		}
		s.snapshotSchemas[table] = schemaFingerprint
	}
}

func (s *PostgresCDCSource) CDCState() source.CDCStateCommitToken {
	s.stateMu.Lock()
	snapshots := make(map[string]string, len(s.snapshotPositions))
	incarnations := make(map[string]string, len(s.snapshotIncarnations))
	schemas := make(map[string]string, len(s.snapshotSchemas))
	for table, position := range s.snapshotPositions {
		snapshots[table] = position
	}
	for table, incarnation := range s.snapshotIncarnations {
		incarnations[table] = incarnation
	}
	for table, fingerprint := range s.snapshotSchemas {
		schemas[table] = fingerprint
	}
	var position string
	if s.caughtUp != nil && s.caughtUp.Committed() > 0 {
		position = FormatLSN(s.caughtUp.Committed())
	}
	s.stateMu.Unlock()
	return source.CDCStateCommitToken{Position: position, SnapshotPositions: snapshots, SnapshotIncarnations: incarnations, SnapshotSchemas: schemas}
}

// ReplicationLag reports how many WAL bytes the durable destination position
// trails the server's WAL head. It is only meaningful while streaming: batch
// runs never call CommitStream, so pos stays zero and the difference would
// report the entire WAL as lag.
func (s *PostgresCDCSource) ReplicationLag() (source.LagSnapshot, bool) {
	if s.lag == nil || s.pos == nil || !s.lag.streaming.Load() {
		return source.LagSnapshot{}, false
	}
	head := s.lag.serverHead.Load()
	if head == 0 {
		return source.LagSnapshot{}, false
	}
	committed := uint64(s.pos.Committed())

	// Saturating: LSN is a uint64, and committed briefly exceeding head after a
	// reconnect would otherwise wrap to ~1.8e19.
	var behind uint64
	if head > committed {
		behind = head - committed
	}

	return source.LagSnapshot{
		Source:          "postgres_cdc",
		BytesBehind:     &behind,
		ServerPosition:  pglogrepl.LSN(head).String(),
		DurablePosition: pglogrepl.LSN(committed).String(),
		CaughtUp:        behind == 0,
		UpdatedAt:       time.Now(),
	}, true
}

// recordCaughtUpLSN records the LSN a batch run has reached. Positions decoded
// from an active replication stream are sent to the slot by FinalizeBatch once
// the destination write is durable. Snapshot-only positions are state markers:
// their connection has not entered CopyBoth mode and must not receive standby
// status updates.
func (s *PostgresCDCSource) recordCaughtUpLSN(lsn pglogrepl.LSN, slotName string, fromStream bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.caughtUp != nil && lsn > s.caughtUp.Committed() {
		s.caughtUp.Commit(lsn)
		s.caughtUpSlot = slotName
		s.caughtUpFromStream = fromStream
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
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	s.stateMu.Lock()
	var lsn pglogrepl.LSN
	if s.caughtUp != nil {
		lsn = s.caughtUp.Committed()
	}
	slotName := s.caughtUpSlot
	fromStream := s.caughtUpFromStream
	s.stateMu.Unlock()
	if lsn == 0 || !fromStream {
		return nil
	}
	if s.replConn == nil {
		return nil
	}
	// Stop the keepalive goroutine before sending so we are the sole writer
	// on the replication connection. The keepalive only sends
	// WALWritePosition (it cannot have advanced the slot); the final send
	// below carries WALFlush=lsn which actually moves confirmed_flush_lsn.
	s.stopKeepalive()
	if slotName == "" {
		return fmt.Errorf("cannot confirm final standby status at LSN %s without a replication slot name", lsn)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	err := sendStandbyStatusUpdate(ctx, s.replConn, pglogrepl.StandbyStatusUpdate{
		WALWritePosition: lsn,
		WALFlushPosition: lsn,
		WALApplyPosition: lsn,
	})
	if err != nil {
		config.Debug("[CDC] Failed to send final standby status at LSN %s: %v", lsn, err)
		return fmt.Errorf("failed to send final standby status: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := s.waitForConfirmedFlushLSN(ctx, slotName, lsn); err != nil {
		return err
	}
	config.Debug("[CDC] Sent final standby status confirming LSN %s", lsn)
	return nil
}

func (s *PostgresCDCSource) waitForConfirmedFlushLSN(ctx context.Context, slotName string, lsn pglogrepl.LSN) error {
	if s.queryPool == nil || s.replConn == nil {
		return fmt.Errorf("cannot confirm final standby status without open PostgreSQL connections")
	}

	waitCtx, cancel := context.WithTimeout(ctx, finalizeConfirmationTimeout)
	defer cancel()
	nextPoll := time.Now()

	for {
		if !time.Now().Before(nextPoll) {
			var confirmed bool
			err := s.queryPool.QueryRow(waitCtx, `
				SELECT COALESCE(confirmed_flush_lsn >= $1::pg_lsn, false)
				FROM pg_replication_slots
				WHERE slot_name = $2
			`, lsn.String(), slotName).Scan(&confirmed)
			if err != nil {
				if waitCtx.Err() != nil {
					return fmt.Errorf("timed out waiting for PostgreSQL to confirm final standby status at LSN %s: %w", lsn, waitCtx.Err())
				}
				return fmt.Errorf("failed to verify final standby status at LSN %s: %w", lsn, err)
			}
			if confirmed {
				return nil
			}
			nextPoll = time.Now().Add(receiveTimeout)
		}

		receiveCtx, receiveCancel := context.WithDeadline(waitCtx, nextPoll)
		_, receiveErr := s.replConn.ReceiveMessage(receiveCtx)
		receiveTimedOut := pgconn.Timeout(receiveErr)
		receiveCancel()
		if waitCtx.Err() != nil {
			return fmt.Errorf("timed out waiting for PostgreSQL to confirm final standby status at LSN %s: %w", lsn, waitCtx.Err())
		}
		if receiveErr != nil && !receiveTimedOut {
			return fmt.Errorf("failed to drain PostgreSQL replication connection while confirming LSN %s: %w", lsn, receiveErr)
		}
	}
}

func (s *PostgresCDCSource) markLegacySlotInUse(slotName string) {
	s.connectorLeaseMu.Lock()
	defer s.connectorLeaseMu.Unlock()
	if _, exists := s.legacySlots[slotName]; exists {
		s.legacySlots[slotName] = true
	}
}

func (s *PostgresCDCSource) FinalizeLegacySlot(ctx context.Context) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	s.connectorLeaseMu.Lock()
	legacySlots := make([]string, 0, len(s.legacySlots))
	for slotName, inUse := range s.legacySlots {
		if !inUse {
			legacySlots = append(legacySlots, slotName)
		}
	}
	s.connectorLeaseMu.Unlock()
	if len(legacySlots) == 0 {
		return nil
	}
	slices.Sort(legacySlots)

	for _, legacySlot := range legacySlots {
		var active bool
		err := s.queryPool.QueryRow(ctx, `
		SELECT active
		FROM pg_replication_slots
		WHERE slot_name = $1 AND slot_type = 'logical'
	`, legacySlot).Scan(&active)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to inspect obsolete legacy replication slot %s: %w", legacySlot, err)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if active {
			return fmt.Errorf("obsolete legacy replication slot %s became active before cutover", legacySlot)
		}
	}

	for _, legacySlot := range legacySlots {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if _, err := s.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", legacySlot); err != nil && !isMissingReplicationSlotError(err) {
			return fmt.Errorf("failed to drop obsolete legacy replication slot %s: %w", legacySlot, err)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		s.connectorLeaseMu.Lock()
		delete(s.legacySlots, legacySlot)
		s.connectorLeaseMu.Unlock()
		config.Debug("[CDC] Dropped obsolete legacy automatic replication slot %s after durable cutover", legacySlot)
	}
	return nil
}

func isMissingReplicationSlotError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42704"
}

func (s *PostgresCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	return NewCDCTable(s, req)
}

func (s *PostgresCDCSource) TableExists(ctx context.Context, table string) (bool, error) {
	schemaName, tableName := parseTableName(table)
	var exists bool
	err := s.queryPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')
		)`, schemaName, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check source table existence: %w", err)
	}
	return exists, nil
}

func parseURIConfig(uri string) (CDCConfig, string, error) {
	var cfg CDCConfig

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
	cfg.StateID = query.Get("state_id")

	if raw := query.Get("binary"); raw != "" {
		switch raw {
		case "true", "1", "on":
			cfg.Binary = true
		case "false", "0", "off":
			cfg.Binary = false
		default:
			return cfg, "", fmt.Errorf("invalid binary option: %s (must be 'true' or 'false')", raw)
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
	query.Del("binary")
	query.Del("state_id")
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
func ensureManagedPublication(ctx context.Context, pool *pgxpool.Pool, pubName string, migrationLocks ...func() error) error {
	included, skipped, err := selectPublishableTables(ctx, pool)
	if err != nil {
		return err
	}

	for _, s := range skipped {
		fmt.Printf("Warning: %s\n", s.warning(pubName))
	}

	tableList := quotePublicationTables(included)
	// Use pgx.Identifier to safely quote identifiers - DDL does not support parameter placeholders.
	pubIdent := pgx.Identifier{pubName}.Sanitize()

	var pubAllTables bool
	err = pool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", pubName).Scan(&pubAllTables)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		config.Debug("[CDC] Creating publication %s for %d table(s)", pubName, len(included))
		if _, err = pool.Exec(ctx, createPublicationSQL(pubIdent, tableList)); err != nil {
			// Another instance may have created it concurrently; fall back to
			// reconciling its table set instead of failing the run.
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && (pgErr.Code == pgErrDuplicate || pgErr.Code == pgErrAlreadyExists) {
				config.Debug("[CDC] Publication %s created concurrently; updating its table set", pubName)
				return reconcilePublicationTables(ctx, pool, pubIdent, tableList, migrationLocks...)
			}
			return fmt.Errorf("failed to create publication: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("failed to check publication existence: %w", err)
	case pubAllTables || len(included) == 0:
		// A publication left over from an older ingestr as FOR ALL TABLES cannot
		// have its table set altered, so recreate it with the explicit list.
		config.Debug("[CDC] Recreating FOR ALL TABLES publication %s with explicit logged-table list", pubName)
		if err := acquirePublicationMigrationLock(migrationLocks); err != nil {
			return err
		}
		return recreatePublication(ctx, pool, pubIdent, tableList)
	default:
		config.Debug("[CDC] Updating publication %s to cover %d table(s)", pubName, len(included))
		return setPublicationTables(ctx, pool, pubIdent, tableList)
	}
}

func createPublicationSQL(pubIdent, tableList string) string {
	if tableList == "" {
		return fmt.Sprintf("CREATE PUBLICATION %s", pubIdent)
	}
	return fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", pubIdent, tableList)
}

func reconcilePublicationTables(ctx context.Context, pool *pgxpool.Pool, pubIdent, tableList string, migrationLocks ...func() error) error {
	if tableList == "" {
		if err := acquirePublicationMigrationLock(migrationLocks); err != nil {
			return err
		}
		return recreatePublication(ctx, pool, pubIdent, tableList)
	}
	return setPublicationTables(ctx, pool, pubIdent, tableList)
}

func acquirePublicationMigrationLock(locks []func() error) error {
	if len(locks) == 0 || locks[0] == nil {
		return nil
	}
	return locks[0]()
}

func recreatePublication(ctx context.Context, pool *pgxpool.Pool, pubIdent, tableList string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin publication migration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, fmt.Sprintf("DROP PUBLICATION %s", pubIdent)); err != nil {
		return fmt.Errorf("failed to drop legacy publication: %w", err)
	}
	if _, err := tx.Exec(ctx, createPublicationSQL(pubIdent, tableList)); err != nil {
		return fmt.Errorf("failed to recreate publication: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit publication migration: %w", err)
	}
	return nil
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
	for key := range query {
		if strings.HasPrefix(key, "pool_") {
			query.Del(key)
		}
	}
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
			SELECT n.nspname, c.relname, c.oid::text
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
			SELECT pt.schemaname, pt.tablename, c.oid::text
			FROM pg_publication_tables pt
			JOIN pg_namespace n ON n.nspname = pt.schemaname
			JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = pt.tablename
			WHERE pt.pubname = $1
			ORDER BY pt.schemaname, pt.tablename
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
		var schemaName, tableName, incarnation string
		if err := rows.Scan(&schemaName, &tableName, &incarnation); err != nil {
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
		fingerprint, err := s.TableSchemaFingerprint(ctx, fullName)
		if err != nil {
			return nil, err
		}

		if len(tableSchema.PrimaryKeys) == 0 {
			s.warnKeylessTable(fullName)
		}

		tables = append(tables, source.SourceTableInfo{
			Name:              fullName,
			Schema:            tableSchema,
			PrimaryKeys:       tableSchema.PrimaryKeys,
			DestSchema:        s.cdcConfig.DestSchema,
			Incarnation:       incarnation,
			SchemaFingerprint: fingerprint,
		})

		config.Debug("[CDC] Found table in publication: %s (PKs: %v)", fullName, tableSchema.PrimaryKeys)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(tables) == 0 {
		config.Debug("[CDC] No tables found. pubAllTables=%v, query used: %s", pubAllTables, query)
		return []source.SourceTableInfo{}, nil
	}

	config.Debug("[CDC] Found %d tables in publication %s", len(tables), s.cdcConfig.Publication)
	return tables, nil
}

// warnKeylessTable tells the user (once per table) that a table with no
// primary key and no replica identity index is ingested as an append-only
// change log instead of a merged mirror.
func (s *PostgresCDCSource) warnKeylessTable(name string) {
	s.keylessWarnedMu.Lock()
	defer s.keylessWarnedMu.Unlock()
	if s.keylessWarned == nil {
		s.keylessWarned = make(map[string]bool)
	}
	if s.keylessWarned[name] {
		return
	}
	s.keylessWarned[name] = true
	fmt.Printf("Warning: table %s has no primary key or replica identity index; ingesting it as an append-only change log (_cdc_deleted marks deletes, updates arrive as delete+insert pairs)\n", name)
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
	incarnations, err := s.listEligibleTableIncarnations(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{}, len(incarnations))
	for name := range incarnations {
		names[name] = struct{}{}
	}
	return names, nil
}

func (s *PostgresCDCSource) listEligibleTableIncarnations(ctx context.Context) (map[string]string, error) {
	names := make(map[string]string)

	if s.managedPublication {
		rows, err := s.queryPool.Query(ctx, `
			SELECT n.nspname, c.relname, c.oid::text
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind = 'r'
			  AND c.relpersistence = 'p'
			  AND `+replicaIdentityClause+`
			  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var schemaName, tableName, incarnation string
			if err := rows.Scan(&schemaName, &tableName, &incarnation); err != nil {
				return nil, fmt.Errorf("failed to scan table identity: %w", err)
			}
			names[publicationTableFullName(schemaName, tableName)] = incarnation
		}
		return names, rows.Err()
	}

	var pubAllTables bool
	err := s.queryPool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", s.cdcConfig.Publication).Scan(&pubAllTables)
	if err != nil {
		return nil, fmt.Errorf("failed to check publication type: %w", err)
	}

	var rows pgx.Rows
	if pubAllTables {
		rows, err = s.queryPool.Query(ctx, `
			SELECT n.nspname, c.relname, c.oid::text
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind = 'r'
			  AND c.relpersistence = 'p'
			  AND `+replicaIdentityClause+`
			  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		`)
	} else {
		rows, err = s.queryPool.Query(ctx, `
			SELECT pt.schemaname, pt.tablename, c.oid::text
			FROM pg_publication_tables pt
			JOIN pg_namespace n ON n.nspname = pt.schemaname
			JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = pt.tablename
			WHERE pt.pubname = $1
		`, s.cdcConfig.Publication)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list publication tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName, incarnation string
		if err := rows.Scan(&schemaName, &tableName, &incarnation); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		names[publicationTableFullName(schemaName, tableName)] = incarnation
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table names: %w", err)
	}
	return names, nil
}

func (s *PostgresCDCSource) validateTablePublished(ctx context.Context, table string) error {
	if s.managedPublication {
		if err := s.reconcileManagedPublication(ctx); err != nil {
			return fmt.Errorf("failed to reconcile managed publication before validating table %q: %w", table, err)
		}
	}
	names, err := s.listEligibleTableNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to validate table %q against publication %q: %w", table, s.cdcConfig.Publication, err)
	}
	schemaName, tableName := parseTableName(table)
	canonical := publicationTableFullName(schemaName, tableName)
	if _, ok := names[canonical]; !ok {
		return fmt.Errorf("table %q is not a publishable member of PostgreSQL publication %q", table, s.cdcConfig.Publication)
	}
	if s.managedPublication {
		var published bool
		if err := s.queryPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_publication_tables
				WHERE pubname = $1 AND schemaname = $2 AND tablename = $3
			)`, s.cdcConfig.Publication, schemaName, tableName).Scan(&published); err != nil {
			return fmt.Errorf("failed to verify managed publication membership for table %q: %w", table, err)
		}
		if !published {
			return fmt.Errorf("table %q is not present in managed PostgreSQL publication %q", table, s.cdcConfig.Publication)
		}
	}
	return nil
}

// reconcilePublication refreshes the managed publication's table set so tables
// created after the stream started become part of it. No-op for user-supplied
// publications, whose membership the user controls.
func (s *PostgresCDCSource) reconcilePublication(ctx context.Context) error {
	if !s.managedPublication {
		return nil
	}
	return s.reconcileManagedPublication(ctx)
}

// ReadAll reads CDC changes from all tables in the publication.
func (s *PostgresCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	tables, err := s.GetTables(ctx)
	if err != nil {
		return nil, err
	}

	if !opts.Streaming && opts.KnownTables != nil {
		tables = filterKnownTables(tables, opts.KnownTables)
	}

	resumeLSNs := make(map[string]string, len(opts.CDCResumeLSNs))
	for table, lsn := range opts.CDCResumeLSNs {
		resumeLSNs[table] = lsn
	}
	var invalidations []source.CDCSnapshotInvalidation
	for _, table := range tables {
		if resumeLSNs[table.Name] == "" {
			continue
		}
		if resumeMetadataChanged(opts.CDCResumeIncarnations[table.Name], opts.CDCResumeSchemaFingerprints[table.Name], table.Incarnation, table.SchemaFingerprint) {
			delete(resumeLSNs, table.Name)
			invalidations = append(invalidations, source.CDCSnapshotInvalidation{TableName: table.Name, Incarnation: table.Incarnation})
		}
	}
	reader := NewMultiTableCDCReader(s, tables, s.cdcConfig, resumeLSNs, opts.CDCSlotSuffix)
	reader.initialInvalidations = invalidations
	if opts.Streaming {
		reader.initialAnnouncements = newlyObservedTables(tables, opts.KnownTables)
	}
	return reader.Read(ctx, opts)
}

func filterKnownTables(tables []source.SourceTableInfo, knownNames []string) []source.SourceTableInfo {
	known := make(map[string]struct{}, len(knownNames))
	for _, name := range knownNames {
		known[name] = struct{}{}
	}
	filtered := make([]source.SourceTableInfo, 0, len(knownNames))
	for _, table := range tables {
		if _, ok := known[table.Name]; ok {
			filtered = append(filtered, table)
		}
	}
	return filtered
}

func newlyObservedTables(tables []source.SourceTableInfo, knownNames []string) []source.SourceTableInfo {
	if knownNames == nil {
		return nil
	}
	known := make(map[string]struct{}, len(knownNames))
	for _, name := range knownNames {
		known[name] = struct{}{}
	}
	var observed []source.SourceTableInfo
	for _, table := range tables {
		if _, ok := known[table.Name]; !ok {
			observed = append(observed, table)
		}
	}
	return observed
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
