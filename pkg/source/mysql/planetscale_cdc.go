package mysql

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	psdbconnect "github.com/bruin-data/ingestr/pkg/source/mysql/internal/psdbconnect"
	"google.golang.org/protobuf/proto"
	vreplication "vitess.io/vitess/go/mysql/replication"
	"vitess.io/vitess/go/sqltypes"
	querypb "vitess.io/vitess/go/vt/proto/query"
	vtrpcpb "vitess.io/vitess/go/vt/proto/vtrpc"
)

// PlanetScaleCDCSource captures changes from a PlanetScale (managed Vitess)
// keyspace. PlanetScale does not expose vtgate's raw VStream port; instead it
// fronts change capture with the psdbconnect gRPC API on the database host over
// TLS/443, authenticated with the database credentials from the URI. Schema/PK/
// shard discovery still uses the MySQL wire protocol.
//
// It emits the same Arrow batches and CDC metadata columns (_cdc_lsn,
// _cdc_deleted, _cdc_synced_at) as the other CDC sources, reusing the in-package
// change-buffer and batching helpers.
type PlanetScaleCDCSource struct {
	db         *sql.DB
	keyspace   string
	destSchema string
	host       string
	username   string
	password   string
}

func NewPlanetScaleCDCSource() *PlanetScaleCDCSource {
	return &PlanetScaleCDCSource{}
}

func (s *PlanetScaleCDCSource) Schemes() []string {
	return []string{"planetscale+cdc"}
}

func (s *PlanetScaleCDCSource) Connect(ctx context.Context, uri string) error {
	cfg, normalizedURI, connInfo, err := parseMySQLCDCURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse PlanetScale CDC URI: %w", err)
	}
	if connInfo.Database == "" {
		return fmt.Errorf("source URI must include a keyspace (database) for PlanetScale CDC")
	}
	if connInfo.Host == "" {
		return fmt.Errorf("source URI must include the PlanetScale host")
	}
	if connInfo.User == "" || connInfo.Password == "" {
		return fmt.Errorf("PlanetScale CDC requires database credentials (user:password) in the source URI")
	}

	dsn, database, err := uriToDSN(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL URI: %w", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open MySQL connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping PlanetScale (vtgate): %w", err)
	}

	s.db = db
	s.keyspace = database
	s.destSchema = cfg.DestSchema
	s.host = connInfo.Host
	s.username = connInfo.User
	s.password = connInfo.Password
	return nil
}

func (s *PlanetScaleCDCSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *PlanetScaleCDCSource) HandlesIncrementality() bool {
	return true
}

func (s *PlanetScaleCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	fullSchema, err := getMySQLSchema(ctx, s.db, s.keyspace, req.Name)
	if err != nil {
		return nil, err
	}
	if err := validateMySQLCDCTableSupported(ctx, s.db, s.keyspace, req.Name); err != nil {
		return nil, err
	}
	tableSchema := addMySQLCDCColumns(fullSchema)

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}
	if len(pks) == 0 {
		return nil, fmt.Errorf("table %s has no primary key; provide --primary-key or add a primary key to the source table", req.Name)
	}
	tableSchema.PrimaryKeys = pks

	strategy := config.StrategyMerge
	if req.Strategy != "" && req.Strategy != config.StrategyReplace {
		strategy = req.Strategy
	}

	return &PlanetScaleCDCTable{
		source:      s,
		tableName:   req.Name,
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    strategy,
	}, nil
}

func (s *PlanetScaleCDCSource) IsMultiTable() bool {
	return true
}

func (s *PlanetScaleCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	return s.getTables(ctx)
}

func (s *PlanetScaleCDCSource) getTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, s.keyspace)
	if err != nil {
		return nil, fmt.Errorf("failed to query PlanetScale tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []source.SourceTableInfo
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan PlanetScale table: %w", err)
		}

		fullSchema, err := getMySQLSchema(ctx, s.db, s.keyspace, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for %s: %w", tableName, err)
		}
		if err := validateMySQLCDCTableSupported(ctx, s.db, s.keyspace, tableName); err != nil {
			return nil, err
		}
		tableSchema := addMySQLCDCColumns(fullSchema)
		if len(tableSchema.PrimaryKeys) == 0 {
			return nil, fmt.Errorf("table %s has no primary key; multi-table PlanetScale CDC requires source primary keys", tableName)
		}

		tables = append(tables, source.SourceTableInfo{
			Name:        tableName,
			Schema:      tableSchema,
			PrimaryKeys: tableSchema.PrimaryKeys,
			DestSchema:  s.destSchema,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no PlanetScale tables found in keyspace %s", s.keyspace)
	}
	return tables, nil
}

func (s *PlanetScaleCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	all, err := s.getTables(ctx)
	if err != nil {
		return nil, err
	}

	filter := map[string]bool{}
	for _, table := range opts.Tables {
		filter[strings.ToLower(table)] = true
	}

	targets := make([]psdbCDCTarget, 0, len(all))
	resumeByTable := make(map[string]string, len(all))
	for _, info := range all {
		if len(filter) > 0 && !filter[strings.ToLower(info.Name)] {
			continue
		}
		_, bare := parseMySQLTableName(s.keyspace, info.Name)
		targets = append(targets, psdbCDCTarget{bareName: bare, resultName: info.Name, schema: info.Schema})
		if lsn := strings.TrimSpace(opts.CDCResumeLSNs[info.Name]); lsn != "" {
			resumeByTable[bare] = lsn
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no PlanetScale tables selected")
	}

	results := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(results)
		if err := s.runPsdbConnect(ctx, targets, resumeByTable, opts.ReadOptions, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// PlanetScaleCDCTable is the single-table SourceTable for PlanetScale CDC.
type PlanetScaleCDCTable struct {
	source      *PlanetScaleCDCSource
	tableName   string
	tableSchema *schema.TableSchema
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

func (t *PlanetScaleCDCTable) Name() string                         { return t.tableName }
func (t *PlanetScaleCDCTable) PrimaryKeys() []string                { return t.primaryKeys }
func (t *PlanetScaleCDCTable) IncrementalKey() string               { return "" }
func (t *PlanetScaleCDCTable) Strategy() config.IncrementalStrategy { return t.strategy }
func (t *PlanetScaleCDCTable) HasKnownSchema() bool                 { return true }

func (t *PlanetScaleCDCTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *PlanetScaleCDCTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	outputSchema := t.tableSchema
	if opts.Schema != nil {
		outputSchema = opts.Schema
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		_, bare := parseMySQLTableName(t.source.keyspace, t.tableName)
		target := psdbCDCTarget{bareName: bare, resultName: "", schema: outputSchema}
		resumeByTable := map[string]string{}
		if lsn := strings.TrimSpace(opts.CDCResumeLSN); lsn != "" {
			resumeByTable[bare] = lsn
		}
		if err := t.source.runPsdbConnect(ctx, []psdbCDCTarget{target}, resumeByTable, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

type psdbCDCTarget struct {
	bareName   string              // table name as passed to the psdbconnect Sync RPC
	resultName string              // RecordBatchResult.TableName tag ("" for single-table)
	schema     *schema.TableSchema // output schema including CDC metadata columns
}

// psdbShardCursor is the persisted position of one shard of one table.
type psdbShardCursor struct {
	Position    string `json:"p,omitempty"`
	LastKnownPk []byte `json:"k,omitempty"` // proto-marshaled *query.QueryResult
}

// psdbCursorState is the per-table cursor persisted into _cdc_lsn. psdbconnect
// streams one table per shard with an independent position, so resume requires a
// position per shard rather than the single cumulative VGTID Vitess VStream uses.
type psdbCursorState struct {
	Shards map[string]psdbShardCursor `json:"s"`
}

func (st psdbCursorState) startCursor(keyspace, shard string) (*psdbconnect.TableCursor, error) {
	cur := &psdbconnect.TableCursor{Keyspace: keyspace, Shard: shard}
	sc, ok := st.Shards[shard]
	if !ok {
		return cur, nil
	}
	cur.Position = sc.Position
	if len(sc.LastKnownPk) > 0 {
		pk := &querypb.QueryResult{}
		if err := proto.Unmarshal(sc.LastKnownPk, pk); err != nil {
			return nil, fmt.Errorf("invalid last_known_pk in resume cursor: %w", err)
		}
		cur.LastKnownPk = pk
		// A pending snapshot resumes by primary key, not GTID.
		cur.Position = ""
	}
	return cur, nil
}

func shardCursorFrom(c *psdbconnect.TableCursor) (psdbShardCursor, error) {
	sc := psdbShardCursor{Position: c.GetPosition()}
	if pk := c.GetLastKnownPk(); pk != nil {
		raw, err := proto.Marshal(pk)
		if err != nil {
			return sc, fmt.Errorf("failed to encode last_known_pk: %w", err)
		}
		sc.LastKnownPk = raw
	}
	return sc, nil
}

func encodePsdbCursor(st psdbCursorState) (string, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("failed to encode PlanetScale cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodePsdbCursor(payload string) (psdbCursorState, error) {
	st := psdbCursorState{Shards: map[string]psdbShardCursor{}}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return st, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if st.Shards == nil {
		st.Shards = map[string]psdbShardCursor{}
	}
	return st, nil
}

// runPsdbConnect streams every selected table across its shards via psdbconnect,
// emitting CDC batches through the shared change buffers. Each table is captured
// independently (its own per-shard cursor persisted in _cdc_lsn), and within a
// table shards are streamed sequentially.
func (s *PlanetScaleCDCSource) runPsdbConnect(ctx context.Context, targets []psdbCDCTarget, resumeByTable map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(targets) == 0 {
		return nil
	}

	shards, err := listVitessShards(ctx, s.db, s.keyspace)
	if err != nil {
		return err
	}
	if len(shards) == 0 {
		return fmt.Errorf("no shards found for keyspace %s", s.keyspace)
	}
	config.Debug("[SOURCE] PlanetScale CDC: keyspace=%q shards=%v tables=%d", s.keyspace, shards, len(targets))

	client, err := psdbconnect.Dial(s.host, s.username, s.password)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	batchSize := mysqlCDCStreamBatchSize(opts)
	buffers := make(map[string]*mysqlCDCChangeBuffer, len(targets))

	for _, t := range targets {
		if err := s.streamTable(ctx, client, t, resumeByTable[t.bareName], shards, batchSize, buffers, results); err != nil {
			return err
		}
	}
	return flushMySQLCDCChangeBuffers(buffers, results)
}

func (s *PlanetScaleCDCSource) streamTable(ctx context.Context, client *psdbconnect.Client, t psdbCDCTarget, resumeLSN string, shards []string, batchSize int, buffers map[string]*mysqlCDCChangeBuffer, results chan<- source.RecordBatchResult) error {
	sourceCols := sourceColumnsWithoutMySQLCDC(t.schema)
	pkPositions := psdbPKPositions(sourceCols, t.schema.PrimaryKeys)

	state := psdbCursorState{Shards: map[string]psdbShardCursor{}}
	var ordinal uint64
	if resumeLSN != "" {
		ord, payload, ok := parseVitessLSN(resumeLSN)
		if !ok {
			return fmt.Errorf("resume position %q for %s is invalid; run with --full-refresh to rebuild the destination safely", resumeLSN, t.bareName)
		}
		decoded, err := decodePsdbCursor(payload)
		if err != nil {
			return fmt.Errorf("resume position for %s is invalid: %w; run with --full-refresh to rebuild the destination safely", t.bareName, err)
		}
		state = decoded
		ordinal = ord + 1
	}

	for _, shard := range shards {
		start, err := state.startCursor(s.keyspace, shard)
		if err != nil {
			return err
		}

		stopPos, err := s.peekPosition(ctx, client, t.bareName, shard)
		if err != nil {
			return err
		}
		config.Debug("[SOURCE] PlanetScale CDC: %s shard=%q startPos=%q hasLastPk=%v stopPos=%q", t.bareName, shard, start.GetPosition(), start.GetLastKnownPk() != nil, stopPos)
		// A resumed shard with no pending snapshot that has already reached the
		// current position has nothing to stream; skip it (and avoid blocking on
		// an idle stream that would never return).
		if start.GetLastKnownPk() == nil && start.GetPosition() != "" && psdbAtLeast(start.GetPosition(), stopPos) {
			config.Debug("[SOURCE] PlanetScale CDC: %s shard=%q already caught up; skipping", t.bareName, shard)
			continue
		}

		if err := s.streamShard(ctx, client, t, shard, start, stopPos, sourceCols, pkPositions, &ordinal, &state, batchSize, buffers, results); err != nil {
			return err
		}
	}
	return nil
}

func (s *PlanetScaleCDCSource) streamShard(ctx context.Context, client *psdbconnect.Client, t psdbCDCTarget, shard string, start *psdbconnect.TableCursor, stopPos string, sourceCols []schema.Column, pkPositions []int, ordinal *uint64, state *psdbCursorState, batchSize int, buffers map[string]*mysqlCDCChangeBuffer, results chan<- source.RecordBatchResult) error {
	stream, err := client.Sync(ctx, &psdbconnect.SyncRequest{
		TableName:      t.bareName,
		Cursor:         start,
		TabletType:     psdbconnect.TabletType_primary,
		IncludeInserts: true,
		IncludeUpdates: true,
		IncludeDeletes: true,
	})
	if err != nil {
		return fmt.Errorf("failed to start psdbconnect Sync for %s/%s: %w", t.bareName, shard, err)
	}

	cursor := start
	// A fresh stream (empty position) or a resumed pending snapshot must run the
	// copy phase before the loop may stop; an incremental resume has no copy.
	copyDone := start.GetPosition() != "" && start.GetLastKnownPk() == nil
	sawLastPk := start.GetLastKnownPk() != nil
	anchor := ""
	pendingCopyStart := -1
	var copyCheckpoint *mysqlCDCChange
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				config.Debug("[SOURCE] PlanetScale CDC: %s/%s stream EOF", t.bareName, shard)
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("psdbconnect Sync receive failed for %s/%s: %w", t.bareName, shard, err)
		}
		if rpcErr := resp.GetError(); rpcErr != nil && rpcErr.GetCode() != vtrpcpb.Code_OK {
			return fmt.Errorf("psdbconnect Sync error for %s/%s: %s", t.bareName, shard, rpcErr.GetMessage())
		}

		changes, err := decodePsdbChanges(resp, sourceCols, pkPositions)
		if err != nil {
			return err
		}

		if c := resp.GetCursor(); c != nil {
			cursor = c
			sc, err := shardCursorFrom(c)
			if err != nil {
				return err
			}
			state.Shards[shard] = sc
		}
		pos := cursor.GetPosition()
		hasLastPk := cursor.GetLastKnownPk() != nil
		if hasLastPk {
			sawLastPk = true
		}
		if anchor == "" && pos != "" {
			anchor = pos
		}

		// Log only on real activity (changes or copy-phase rows); heartbeat-only
		// responses advance the GTID constantly and would otherwise flood --debug.
		if len(changes) > 0 || hasLastPk {
			config.Debug("[SOURCE] PlanetScale CDC: %s/%s resp result=%d updates=%d deletes=%d changes=%d pos=%q hasLastPk=%v copyDone=%v",
				t.bareName, shard, len(resp.GetResult()), len(resp.GetUpdates()), len(resp.GetDeletes()), len(changes), pos, hasLastPk, copyDone)
		}

		// The snapshot is finished once the per-row primary-key checkpoint clears
		// after appearing, or (for an empty table) the position advances past the
		// snapshot's anchor.
		if !copyDone && psdbCopyFinished(sawLastPk, pos, anchor, hasLastPk) {
			copyDone = true
			payload, err := encodePsdbCursor(*state)
			if err != nil {
				return err
			}
			if psdbRewriteBufferedLSNs(buffers, t.bareName, pendingCopyStart, *ordinal, payload) {
				(*ordinal)++
			} else if copyCheckpoint != nil {
				checkpoint := *copyCheckpoint
				checkpoint.lsn = formatVitessLSN(*ordinal, 0, payload)
				if err := appendMySQLCDCBufferedChanges(buffers, t.bareName, t.schema, t.resultName, []mysqlCDCChange{checkpoint}, batchSize, results); err != nil {
					return err
				}
				(*ordinal)++
			}
			pendingCopyStart = -1
			copyCheckpoint = nil
		}

		if len(changes) > 0 {
			payload, err := encodePsdbCursor(*state)
			if err != nil {
				return err
			}
			for i := range changes {
				changes[i].lsn = formatVitessLSN(*ordinal, i, payload)
			}
			copyPhase := !copyDone && hasLastPk
			beforeLen := 0
			if copyPhase {
				if buffer := buffers[t.bareName]; buffer != nil {
					beforeLen = len(buffer.changes)
				}
				checkpoint := changes[len(changes)-1]
				copyCheckpoint = &checkpoint
			}
			if err := appendMySQLCDCBufferedChanges(buffers, t.bareName, t.schema, t.resultName, changes, batchSize, results); err != nil {
				return err
			}
			if copyPhase {
				if buffer := buffers[t.bareName]; buffer != nil && len(buffer.changes) >= beforeLen+len(changes) {
					if pendingCopyStart < 0 {
						pendingCopyStart = beforeLen
					}
				} else {
					pendingCopyStart = -1
				}
			}
			(*ordinal)++
		}

		// In batch mode, stop once the snapshot is done and the stream has caught
		// up to the position observed when it started (at or beyond stopPos). The
		// current response's changes have already been emitted above, so returning
		// here is safe even when this very response carried the shard's final
		// change and landed on stopPos — see psdbReachedStop for why we must not
		// wait for a separate empty response.
		if psdbReachedStop(copyDone, hasLastPk, pos, stopPos) {
			config.Debug("[SOURCE] PlanetScale CDC: %s/%s stop: caught up (pos=%q >= stop=%q)", t.bareName, shard, pos, stopPos)
			return nil
		}
	}
}

func psdbCopyFinished(sawLastPk bool, pos, anchor string, hasLastPk bool) bool {
	return !hasLastPk && (sawLastPk || (pos != "" && anchor != "" && pos != anchor))
}

// psdbReachedStop reports whether a batch-mode shard stream has captured
// everything up to the boundary observed when it started (stopPos) and may
// return. It deliberately does NOT require a change-free response: the response
// carrying a shard's final change usually lands exactly on stopPos, and on an
// otherwise-idle shard no further heartbeat response ever arrives — waiting for
// one would block Recv forever (and, since shards stream sequentially, stall
// every later shard). Callers emit the current response's changes before
// checking this, so stopping here loses nothing.
func psdbReachedStop(copyDone, hasLastPk bool, pos, stopPos string) bool {
	return copyDone && !hasLastPk && psdbAtLeast(pos, stopPos)
}

func psdbRewriteBufferedLSNs(buffers map[string]*mysqlCDCChangeBuffer, key string, start int, ordinal uint64, payload string) bool {
	if start < 0 {
		return false
	}
	buffer := buffers[key]
	if buffer == nil || start >= len(buffer.changes) {
		return false
	}
	for i := start; i < len(buffer.changes); i++ {
		buffer.changes[i].lsn = formatVitessLSN(ordinal, i-start, payload)
	}
	return true
}

// peekPosition opens a short-lived Sync at the special "current" position to read
// the shard's latest VGTID, used as the stop boundary for batch capture.
func (s *PlanetScaleCDCSource) peekPosition(ctx context.Context, client *psdbconnect.Client, table, shard string) (string, error) {
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := client.Sync(pctx, &psdbconnect.SyncRequest{
		TableName:  table,
		Cursor:     &psdbconnect.TableCursor{Keyspace: s.keyspace, Shard: shard, Position: "current"},
		TabletType: psdbconnect.TabletType_primary,
	})
	if err != nil {
		return "", fmt.Errorf("failed to peek psdbconnect position for %s/%s: %w", table, shard, err)
	}
	resp, err := stream.Recv()
	if err != nil {
		return "", fmt.Errorf("failed to read psdbconnect peek for %s/%s: %w", table, shard, err)
	}
	if c := resp.GetCursor(); c != nil {
		return c.GetPosition(), nil
	}
	return "", fmt.Errorf("psdbconnect peek for %s/%s returned no cursor", table, shard)
}

// decodePsdbChanges turns a SyncResponse into ordered CDC changes. Inserts and
// the after-image of updates become upserts; deletes (which carry only primary
// keys) and the before-image of a primary-key-changing update become tombstones.
func decodePsdbChanges(resp *psdbconnect.SyncResponse, sourceCols []schema.Column, pkPositions []int) ([]mysqlCDCChange, error) {
	var changes []mysqlCDCChange

	for _, qr := range resp.GetResult() {
		rows, err := psdbResultRows(qr, sourceCols)
		if err != nil {
			return nil, err
		}
		for _, vals := range rows {
			changes = append(changes, mysqlCDCChange{values: vals, deleted: false})
		}
	}

	for _, up := range resp.GetUpdates() {
		beforeRows, err := psdbResultRows(up.GetBefore(), sourceCols)
		if err != nil {
			return nil, err
		}
		afterRows, err := psdbResultRows(up.GetAfter(), sourceCols)
		if err != nil {
			return nil, err
		}
		for i, after := range afterRows {
			if i < len(beforeRows) && psdbPKChanged(beforeRows[i], after, pkPositions) {
				changes = append(changes, mysqlCDCChange{values: beforeRows[i], deleted: true})
			}
			changes = append(changes, mysqlCDCChange{values: after, deleted: false})
		}
	}

	for _, del := range resp.GetDeletes() {
		rows, err := psdbResultRows(del.GetResult(), sourceCols)
		if err != nil {
			return nil, err
		}
		for _, vals := range rows {
			changes = append(changes, mysqlCDCChange{values: vals, deleted: true})
		}
	}

	return changes, nil
}

// psdbResultRows decodes a query.QueryResult into source-column-ordered values.
// Columns absent from the result (e.g. non-PK columns of a delete tombstone)
// become nil, which the merge strategy resolves by primary key.
func psdbResultRows(qr *querypb.QueryResult, sourceCols []schema.Column) ([][]interface{}, error) {
	if qr == nil || len(qr.Rows) == 0 {
		return nil, nil
	}
	idxByName := make(map[string]int, len(qr.Fields))
	for i, f := range qr.Fields {
		idxByName[strings.ToLower(f.Name)] = i
	}
	out := make([][]interface{}, 0, len(qr.Rows))
	for _, row := range qr.Rows {
		vals := sqltypes.MakeRowTrusted(qr.Fields, row)
		decoded := make([]interface{}, len(sourceCols))
		for i, col := range sourceCols {
			idx, ok := idxByName[strings.ToLower(col.Name)]
			if !ok || idx < 0 || idx >= len(vals) {
				decoded[i] = nil
				continue
			}
			v := vals[idx]
			if v.IsNull() {
				decoded[i] = nil
				continue
			}
			if col.DataType == schema.TypeBinary {
				raw := v.Raw()
				cp := make([]byte, len(raw))
				copy(cp, raw)
				decoded[i] = cp
				continue
			}
			decoded[i] = v.ToString()
		}
		out = append(out, decoded)
	}
	return out, nil
}

func psdbPKPositions(sourceCols []schema.Column, primaryKeys []string) []int {
	out := make([]int, 0, len(primaryKeys))
	for _, pk := range primaryKeys {
		for i, col := range sourceCols {
			if strings.EqualFold(col.Name, pk) {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

func psdbPKChanged(before, after []interface{}, pkPositions []int) bool {
	for _, idx := range pkPositions {
		if idx < 0 || idx >= len(before) || idx >= len(after) {
			continue
		}
		if !reflect.DeepEqual(before[idx], after[idx]) {
			return true
		}
	}
	return false
}

// psdbAtLeast reports whether position pos is at or beyond stop, comparing as
// Vitess GTID sets and falling back to string equality when unparseable.
func psdbAtLeast(pos, stop string) bool {
	if pos == "" || stop == "" {
		return false
	}
	if pos == stop {
		return true
	}
	posSet, err := vreplication.DecodePosition(pos)
	if err != nil {
		return false
	}
	stopSet, err := vreplication.DecodePosition(stop)
	if err != nil {
		return false
	}
	return posSet.AtLeast(stopSet)
}

var (
	_ source.Source           = (*PlanetScaleCDCSource)(nil)
	_ source.MultiTableSource = (*PlanetScaleCDCSource)(nil)
	_ source.SourceTable      = (*PlanetScaleCDCTable)(nil)
)
