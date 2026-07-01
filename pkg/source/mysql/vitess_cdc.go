package mysql

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"vitess.io/vitess/go/sqltypes"
	"vitess.io/vitess/go/vt/grpcclient"
	binlogdatapb "vitess.io/vitess/go/vt/proto/binlogdata"
	querypb "vitess.io/vitess/go/vt/proto/query"
	topodatapb "vitess.io/vitess/go/vt/proto/topodata"
	vtgatepb "vitess.io/vitess/go/vt/proto/vtgate"
	vtgateservicepb "vitess.io/vitess/go/vt/proto/vtgateservice"
)

// VitessCDCSource captures changes from a Vitess keyspace using the VStream gRPC
// API exposed by vtgate. Vitess does not expose a standard MySQL binary log, so
// the binlog-based MySQLCDCSource cannot be used; VStream provides a consistent
// copy (snapshot) phase followed by a streaming phase, with position tracked by
// a VGTID (a set of per-shard GTIDs).
//
// It emits the same Arrow batches and CDC metadata columns (_cdc_lsn,
// _cdc_deleted, _cdc_synced_at) as the other CDC sources, reusing the in-package
// change-buffer and batching helpers. Two connections are used: a MySQL
// connection over vtgate's wire protocol for schema/PK discovery, and a vtgate
// gRPC connection for change capture.
type VitessCDCSource struct {
	db         *sql.DB
	keyspace   string
	destSchema string
	grpcTarget string
	grpcCreds  credentials.TransportCredentials
}

func NewVitessCDCSource() *VitessCDCSource {
	return &VitessCDCSource{}
}

func (s *VitessCDCSource) Schemes() []string {
	return []string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"}
}

func (s *VitessCDCSource) Connect(ctx context.Context, uri string) error {
	cfg, normalizedURI, connInfo, err := parseMySQLCDCURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Vitess CDC URI: %w", err)
	}
	if connInfo.Database == "" {
		return fmt.Errorf("source URI must include a keyspace for Vitess CDC")
	}

	grpcTarget, err := vitessGRPCTarget(uri, connInfo.Host)
	if err != nil {
		return err
	}

	grpcCreds, err := vitessGRPCTLSCredentials(uri)
	if err != nil {
		return err
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
		return fmt.Errorf("failed to ping Vitess (vtgate): %w", err)
	}

	s.db = db
	s.keyspace = database
	s.destSchema = cfg.DestSchema
	s.grpcTarget = grpcTarget
	s.grpcCreds = grpcCreds
	return nil
}

func (s *VitessCDCSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *VitessCDCSource) HandlesIncrementality() bool {
	return true
}

func (s *VitessCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

	return &VitessCDCTable{
		source:      s,
		tableName:   req.Name,
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    strategy,
	}, nil
}

func (s *VitessCDCSource) IsMultiTable() bool {
	return true
}

func (s *VitessCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	return s.getTables(ctx)
}

func (s *VitessCDCSource) getTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, s.keyspace)
	if err != nil {
		return nil, fmt.Errorf("failed to query Vitess tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []source.SourceTableInfo
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan Vitess table: %w", err)
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
			return nil, fmt.Errorf("table %s has no primary key; multi-table Vitess CDC requires source primary keys", tableName)
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
		return nil, fmt.Errorf("no Vitess tables found in keyspace %s", s.keyspace)
	}
	return tables, nil
}

func (s *VitessCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	all, err := s.getTables(ctx)
	if err != nil {
		return nil, err
	}

	filter := map[string]bool{}
	for _, table := range opts.Tables {
		filter[strings.ToLower(table)] = true
	}

	targets := make([]vitessCDCTarget, 0, len(all))
	resumeByBare := make(map[string]string, len(all))
	for _, info := range all {
		if len(filter) > 0 && !filter[strings.ToLower(info.Name)] {
			continue
		}
		_, bare := parseMySQLTableName(s.keyspace, info.Name)
		targets = append(targets, vitessCDCTarget{bareName: bare, resultName: info.Name, schema: info.Schema})
		if lsn := strings.TrimSpace(opts.CDCResumeLSNs[info.Name]); lsn != "" {
			resumeByBare[bare] = lsn
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no Vitess tables selected")
	}

	results := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(results)
		if err := s.runVStream(ctx, targets, resumeByBare, opts.ReadOptions, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// VitessCDCTable is the single-table SourceTable for Vitess CDC.
type VitessCDCTable struct {
	source      *VitessCDCSource
	tableName   string
	tableSchema *schema.TableSchema
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

func (t *VitessCDCTable) Name() string                         { return t.tableName }
func (t *VitessCDCTable) PrimaryKeys() []string                { return t.primaryKeys }
func (t *VitessCDCTable) IncrementalKey() string               { return "" }
func (t *VitessCDCTable) Strategy() config.IncrementalStrategy { return t.strategy }
func (t *VitessCDCTable) HasKnownSchema() bool                 { return true }

func (t *VitessCDCTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *VitessCDCTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	outputSchema := t.tableSchema
	if opts.Schema != nil {
		outputSchema = opts.Schema
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		_, bare := parseMySQLTableName(t.source.keyspace, t.tableName)
		target := vitessCDCTarget{bareName: bare, resultName: "", schema: outputSchema}
		resumeByBare := map[string]string{}
		if lsn := strings.TrimSpace(opts.CDCResumeLSN); lsn != "" {
			resumeByBare[bare] = lsn
		}
		if err := t.source.runVStream(ctx, []vitessCDCTarget{target}, resumeByBare, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

type vitessCDCTarget struct {
	bareName   string              // table name as it appears in VStream events
	resultName string              // RecordBatchResult.TableName tag ("" for single-table)
	schema     *schema.TableSchema // output schema including CDC metadata columns
}

type vitessFieldInfo struct {
	fields    []*querypb.Field
	idxByName map[string]int
}

type vitessTxnRow struct {
	bareName string
	values   []interface{}
	deleted  bool
}

// runVStream opens a single VStream covering all target tables and drives the
// copy + streaming phases, emitting CDC batches via the shared change buffers.
// In batch mode it stops once caught up: heartbeats are only emitted when the
// stream is idle, so the first heartbeat after the copy phase completes (or
// immediately, when resuming) signals that everything up to "now" was streamed.
func (s *VitessCDCSource) runVStream(ctx context.Context, targets []vitessCDCTarget, resumeByBare map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(targets) == 0 {
		return nil
	}

	schemaByTable := make(map[string]*schema.TableSchema, len(targets))
	resultByTable := make(map[string]string, len(targets))
	rules := make([]*binlogdatapb.Rule, 0, len(targets))
	for _, t := range targets {
		schemaByTable[t.bareName] = t.schema
		resultByTable[t.bareName] = t.resultName
		rules = append(rules, &binlogdatapb.Rule{Match: t.bareName, Filter: "select * from " + t.bareName})
	}

	shards, err := s.listShards(ctx)
	if err != nil {
		return err
	}
	if len(shards) == 0 {
		return fmt.Errorf("no shards found for keyspace %s", s.keyspace)
	}

	startVGtid, ordinal, fromResume, err := s.resolveVitessStart(targets, resumeByBare, shards)
	if err != nil {
		return err
	}

	flags := &vtgatepb.VStreamFlags{HeartbeatInterval: 1, StopOnReshard: false}

	// vtgateconn.Dial is unusable for TLS: its registered gRPC dialer appends an
	// insecure transport credential that overrides whatever we pass, so a TLS-only
	// vtgate (e.g. PlanetScale) is unreachable through it. Dial the vtgate service
	// directly with the resolved credentials. grpcclient.DialContext is reused so
	// the connection keeps Vitess's tuned defaults (16MB max message size,
	// keepalive), which matter for large copy-phase VStream messages.
	cc, err := grpcclient.DialContext(ctx, s.grpcTarget, grpcclient.FailFast(false), grpc.WithTransportCredentials(s.grpcCreds))
	if err != nil {
		return fmt.Errorf("failed to connect to vtgate gRPC at %s: %w", s.grpcTarget, err)
	}
	defer func() { _ = cc.Close() }()

	reader, err := vtgateservicepb.NewVitessClient(cc).VStream(ctx, &vtgatepb.VStreamRequest{
		TabletType: topodatapb.TabletType_PRIMARY,
		Vgtid:      startVGtid,
		Filter:     &binlogdatapb.Filter{Rules: rules},
		Flags:      flags,
	})
	if err != nil {
		return fmt.Errorf("failed to start Vitess VStream: %w", err)
	}

	batchSize := mysqlCDCStreamBatchSize(opts)
	buffers := make(map[string]*mysqlCDCChangeBuffer, len(targets))
	fieldsByTable := make(map[string]*vitessFieldInfo)
	var latestVGtid *binlogdatapb.VGtid
	var txnRows []vitessTxnRow
	readyToStop := fromResume // resume runs have no copy phase to wait for

	flushTxn := func() error {
		if len(txnRows) == 0 || latestVGtid == nil {
			return nil
		}
		payload, err := encodeVitessVGtid(latestVGtid)
		if err != nil {
			return err
		}
		for i, r := range txnRows {
			change := mysqlCDCChange{
				values:  r.values,
				lsn:     formatVitessLSN(ordinal, i, payload),
				deleted: r.deleted,
			}
			if err := appendMySQLCDCBufferedChanges(buffers, r.bareName, schemaByTable[r.bareName], resultByTable[r.bareName], []mysqlCDCChange{change}, batchSize, results); err != nil {
				return err
			}
		}
		ordinal++
		txnRows = txnRows[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := reader.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if ferr := flushTxn(); ferr != nil {
					return ferr
				}
				return flushMySQLCDCChangeBuffers(buffers, results)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("vstream receive failed: %w", err)
		}

		for _, ev := range resp.Events {
			switch ev.Type {
			case binlogdatapb.VEventType_FIELD:
				fe := ev.FieldEvent
				if fe == nil {
					continue
				}
				info := &vitessFieldInfo{fields: fe.Fields, idxByName: make(map[string]int, len(fe.Fields))}
				for i, f := range fe.Fields {
					info.idxByName[strings.ToLower(f.Name)] = i
				}
				fieldsByTable[s.bareTableName(fe.TableName)] = info

			case binlogdatapb.VEventType_ROW:
				re := ev.RowEvent
				if re == nil {
					continue
				}
				// VStream reports keyspace-qualified table names (e.g. "vtdb.items");
				// our keys are bare table names.
				bare := s.bareTableName(re.TableName)
				outSchema := schemaByTable[bare]
				info := fieldsByTable[bare]
				if outSchema == nil || info == nil {
					continue
				}
				rows, derr := vitessDecodeRowChanges(bare, re, outSchema, info)
				if derr != nil {
					return derr
				}
				txnRows = append(txnRows, rows...)

			case binlogdatapb.VEventType_VGTID:
				if ev.Vgtid != nil {
					latestVGtid = ev.Vgtid
				}
				// The copy phase delimits row batches with VGTID (from LASTPK)
				// events and sends no COMMIT, so flush here to bound memory and
				// capture copy rows. During streaming txnRows is already empty
				// at this point (the prior COMMIT flushed it), so this is a no-op.
				if err := flushTxn(); err != nil {
					return err
				}

			case binlogdatapb.VEventType_COMMIT:
				if err := flushTxn(); err != nil {
					return err
				}

			case binlogdatapb.VEventType_COPY_COMPLETED:
				readyToStop = true

			case binlogdatapb.VEventType_HEARTBEAT:
				if ev.Vgtid != nil {
					latestVGtid = ev.Vgtid
				}
				if readyToStop {
					if err := flushTxn(); err != nil {
						return err
					}
					return flushMySQLCDCChangeBuffers(buffers, results)
				}
			}
		}
	}
}

// resolveVitessStart determines the VStream start position and seeds the LSN
// ordinal from stored resume cursors. VStream's VGTID is cumulative (every
// commit carries the latest GTID for all shards), so persisting and resuming the
// whole VGTID handles both unsharded and sharded keyspaces with one cursor.
func (s *VitessCDCSource) resolveVitessStart(targets []vitessCDCTarget, resumeByBare map[string]string, shards []string) (*binlogdatapb.VGtid, uint64, bool, error) {
	haveAll := true
	anyResume := false
	var minOrdinal uint64 = math.MaxUint64
	var maxOrdinal uint64
	var oldest *binlogdatapb.VGtid

	for _, t := range targets {
		lsn := strings.TrimSpace(resumeByBare[t.bareName])
		if lsn == "" {
			haveAll = false
			continue
		}
		ord, payload, ok := parseVitessLSN(lsn)
		if !ok {
			return nil, 0, false, fmt.Errorf("resume position %q for %s is invalid; run with --full-refresh to rebuild the destination safely", lsn, t.bareName)
		}
		vgtid, err := decodeVitessVGtid(payload)
		if err != nil {
			return nil, 0, false, fmt.Errorf("resume position for %s is invalid: %w; run with --full-refresh to rebuild the destination safely", t.bareName, err)
		}
		anyResume = true
		if ord < minOrdinal {
			minOrdinal = ord
			oldest = vgtid
		}
		if ord > maxOrdinal {
			maxOrdinal = ord
		}
	}

	ordinal := uint64(0)
	if anyResume {
		ordinal = maxOrdinal + 1
	}

	// Resume the whole stream only when every selected table has a cursor; merge
	// makes any re-delivery to already-advanced tables idempotent.
	if anyResume && haveAll {
		return oldest, ordinal, true, nil
	}
	if anyResume && !haveAll {
		config.Debug("[SOURCE] Vitess CDC: some selected tables lack a resume cursor; performing a fresh copy for all selected tables")
	}

	// Fresh (or mixed) run: empty Gtid triggers VStream's consistent copy phase.
	// Shards are listed explicitly (one ShardGtid each) rather than relying on the
	// empty-Shard "all shards" expansion, which vtcombo resolves unreliably. An
	// unsharded keyspace has a single shard named "-".
	shardGtids := make([]*binlogdatapb.ShardGtid, 0, len(shards))
	for _, sh := range shards {
		shardGtids = append(shardGtids, &binlogdatapb.ShardGtid{Keyspace: s.keyspace, Shard: sh, Gtid: ""})
	}
	fresh := &binlogdatapb.VGtid{ShardGtids: shardGtids}
	return fresh, ordinal, false, nil
}

// listShards returns the shard names of the keyspace (e.g. ["-"] when unsharded,
// ["-80", "80-"] when sharded) via vtgate's SHOW VITESS_SHARDS.
func (s *VitessCDCSource) listShards(ctx context.Context) ([]string, error) {
	return listVitessShards(ctx, s.db, s.keyspace)
}

// listVitessShards reports the shard names of a keyspace via vtgate's SHOW
// VITESS_SHARDS. It is shared by the Vitess VStream and PlanetScale psdbconnect
// CDC sources, which both speak the MySQL wire protocol to a vtgate.
func listVitessShards(ctx context.Context, db *sql.DB, keyspace string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW VITESS_SHARDS")
	if err != nil {
		return nil, fmt.Errorf("failed to list Vitess shards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	prefix := keyspace + "/"
	var shards []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			return nil, fmt.Errorf("failed to scan Vitess shard: %w", err)
		}
		entry = strings.TrimSpace(entry)
		if name, ok := strings.CutPrefix(entry, prefix); ok {
			shards = append(shards, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shards, nil
}

func vitessDecodeRowChanges(bareName string, re *binlogdatapb.RowEvent, outSchema *schema.TableSchema, info *vitessFieldInfo) ([]vitessTxnRow, error) {
	sourceCols := sourceColumnsWithoutMySQLCDC(outSchema)
	pkIdx := vitessPKFieldIndexes(outSchema.PrimaryKeys, info.idxByName)

	out := make([]vitessTxnRow, 0, len(re.RowChanges))
	for _, rc := range re.RowChanges {
		before, after := rc.Before, rc.After
		switch {
		case after != nil && before == nil: // INSERT (also copy-phase rows)
			vals, err := vitessRowValues(info.fields, after, sourceCols, info.idxByName)
			if err != nil {
				return nil, err
			}
			out = append(out, vitessTxnRow{bareName: bareName, values: vals, deleted: false})

		case after == nil && before != nil: // DELETE
			vals, err := vitessRowValues(info.fields, before, sourceCols, info.idxByName)
			if err != nil {
				return nil, err
			}
			out = append(out, vitessTxnRow{bareName: bareName, values: vals, deleted: true})

		case after != nil && before != nil: // UPDATE
			if vitessPKChanged(info.fields, before, after, pkIdx) {
				vals, err := vitessRowValues(info.fields, before, sourceCols, info.idxByName)
				if err != nil {
					return nil, err
				}
				out = append(out, vitessTxnRow{bareName: bareName, values: vals, deleted: true})
			}
			vals, err := vitessRowValues(info.fields, after, sourceCols, info.idxByName)
			if err != nil {
				return nil, err
			}
			out = append(out, vitessTxnRow{bareName: bareName, values: vals, deleted: false})
		}
	}
	return out, nil
}

// bareTableName strips the leading "keyspace." that VStream prefixes onto event
// table names, yielding the bare table name used as our internal map key.
func (s *VitessCDCSource) bareTableName(name string) string {
	if after, ok := strings.CutPrefix(name, s.keyspace+"."); ok {
		return after
	}
	return name
}

// vitessRowValues decodes a VStream row into source-column-ordered Go values,
// mirroring convertMySQLCDCValue: binary columns keep their raw bytes, everything
// else becomes a string that arrowconv coerces into the target Arrow type.
func vitessRowValues(fields []*querypb.Field, row *querypb.Row, sourceCols []schema.Column, idxByName map[string]int) ([]interface{}, error) {
	vals := sqltypes.MakeRowTrusted(fields, row)
	out := make([]interface{}, len(sourceCols))
	for i, col := range sourceCols {
		idx, ok := idxByName[strings.ToLower(col.Name)]
		if !ok {
			return nil, fmt.Errorf("VStream field for column %q not found in table schema", col.Name)
		}
		if idx < 0 || idx >= len(vals) {
			out[i] = nil
			continue
		}
		v := vals[idx]
		if v.IsNull() {
			out[i] = nil
			continue
		}
		if col.DataType == schema.TypeBinary {
			raw := v.Raw()
			cp := make([]byte, len(raw))
			copy(cp, raw)
			out[i] = cp
			continue
		}
		out[i] = v.ToString()
	}
	return out, nil
}

func vitessPKFieldIndexes(primaryKeys []string, idxByName map[string]int) []int {
	out := make([]int, 0, len(primaryKeys))
	for _, pk := range primaryKeys {
		if idx, ok := idxByName[strings.ToLower(pk)]; ok {
			out = append(out, idx)
		}
	}
	return out
}

func vitessPKChanged(fields []*querypb.Field, before, after *querypb.Row, pkIdx []int) bool {
	b := sqltypes.MakeRowTrusted(fields, before)
	a := sqltypes.MakeRowTrusted(fields, after)
	for _, idx := range pkIdx {
		if idx < 0 || idx >= len(b) || idx >= len(a) {
			continue
		}
		if b[idx].ToString() != a[idx].ToString() {
			return true
		}
	}
	return false
}

func vitessGRPCTarget(rawURI string, defaultHost string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	port := strings.TrimSpace(q.Get("grpc_port"))
	if port == "" {
		return "", fmt.Errorf("CDC over Vitess requires the vtgate gRPC port; add ?grpc_port=<port> to the source URI (it differs from the MySQL protocol port)")
	}
	host := strings.TrimSpace(q.Get("grpc_host"))
	if host == "" {
		host = defaultHost
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// vitessGRPCTLSCredentials resolves the transport credentials for the VStream
// gRPC connection. grpc_tls, when set, takes precedence; otherwise the gRPC side
// inherits the MySQL-protocol tls parameter so a single tls=true secures both
// connections. Only true and skip-verify enable TLS (skip-verify skips
// certificate verification); false, preferred, a custom go-sql-driver TLS config
// name, or an unset value leave the gRPC connection plaintext.
func vitessGRPCTLSCredentials(rawURI string) (credentials.TransportCredentials, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, err
	}
	q := u.Query()

	mode := strings.TrimSpace(strings.ToLower(q.Get("grpc_tls")))
	if mode == "" {
		mode = strings.TrimSpace(strings.ToLower(q.Get("tls")))
	}

	switch mode {
	case "true", "skip-verify":
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if mode == "skip-verify" {
			cfg.InsecureSkipVerify = true
		}
		return credentials.NewTLS(cfg), nil
	default:
		return insecure.NewCredentials(), nil
	}
}

var vitessLSNRegex = regexp.MustCompile(`^(\d{20}):(\d{6}):(.+)$`)

func formatVitessLSN(ordinal uint64, rowSeq int, payload string) string {
	return fmt.Sprintf("%020d:%06d:%s", ordinal, rowSeq, payload)
}

func parseVitessLSN(stored string) (uint64, string, bool) {
	m := vitessLSNRegex.FindStringSubmatch(strings.TrimSpace(stored))
	if len(m) != 4 {
		return 0, "", false
	}
	ord, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return ord, m[3], true
}

func encodeVitessVGtid(v *binlogdatapb.VGtid) (string, error) {
	raw, err := proto.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("failed to encode VGTID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeVitessVGtid(payload string) (*binlogdatapb.VGtid, error) {
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid VGTID payload: %w", err)
	}
	v := &binlogdatapb.VGtid{}
	if err := proto.Unmarshal(raw, v); err != nil {
		return nil, fmt.Errorf("invalid VGTID payload: %w", err)
	}
	return v, nil
}

var (
	_ source.Source           = (*VitessCDCSource)(nil)
	_ source.MultiTableSource = (*VitessCDCSource)(nil)
	_ source.SourceTable      = (*VitessCDCTable)(nil)
)
