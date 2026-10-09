package mysql

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	psdbconnect "github.com/bruin-data/ingestr/pkg/source/mysql/internal/psdbconnect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"vitess.io/vitess/go/sqltypes"
	querypb "vitess.io/vitess/go/vt/proto/query"
)

func TestParseMySQLCDCURIPlanetScaleParams(t *testing.T) {
	uri := "ps_mysql+cdc://user:pscale_pw_secret@abc.connect.psdb.cloud:3306/mydb"

	_, normalized, connInfo, err := parseMySQLCDCURI(uri)
	if err != nil {
		t.Fatalf("parseMySQLCDCURI: %v", err)
	}

	if connInfo.Host != "abc.connect.psdb.cloud" {
		t.Errorf("Host: got %q", connInfo.Host)
	}
	if connInfo.Database != "mydb" {
		t.Errorf("Database: got %q", connInfo.Database)
	}
	// psdbconnect authenticates with the database credentials from the URI.
	if connInfo.User != "user" || connInfo.Password != "pscale_pw_secret" {
		t.Errorf("credentials: got %q:%q", connInfo.User, connInfo.Password)
	}

	// The +cdc suffix is stripped, leaving the ps_mysql scheme so uriToDSN can
	// auto-enable TLS for the underlying MySQL connection.
	if !strings.HasPrefix(normalized, "ps_mysql://") {
		t.Errorf("normalized URI must keep the ps_mysql scheme: %s", normalized)
	}
}

// TestCDCSchemeRouting verifies each scheme resolves to its dedicated backend via
// the registry, replacing the old probe-based dispatch.
func TestCDCSchemeRouting(t *testing.T) {
	cases := []struct {
		scheme string
		want   interface{}
	}{
		{"mysql", (*MySQLSource)(nil)},
		{"vitess", (*VitessSource)(nil)},
		{"ps_mysql", (*VitessSource)(nil)},
		{"mysql+cdc", (*MySQLCDCSource)(nil)},
		{"vitess+cdc", (*VitessCDCSource)(nil)},
		{"ps_mysql+cdc", (*PlanetScaleCDCSource)(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.scheme, func(t *testing.T) {
			ctor, err := registry.Default.GetSourceConstructor(tc.scheme)
			if err != nil {
				t.Fatalf("GetSourceConstructor(%q): %v", tc.scheme, err)
			}
			if got := ctor(); reflect.TypeOf(got) != reflect.TypeOf(tc.want) {
				t.Errorf("scheme %q routed to %T, want %T", tc.scheme, got, tc.want)
			}
		})
	}
}

func TestPlanetScaleCDCAdvertisesStreaming(t *testing.T) {
	streaming, ok := any(NewPlanetScaleCDCSource()).(source.StreamingSource)
	if !ok {
		t.Fatal("PlanetScale CDC should implement StreamingSource")
	}
	if !streaming.SupportsStreaming() {
		t.Fatal("PlanetScale CDC should support streaming")
	}
	if got := streaming.DefaultStreamingStrategy(); got != config.StrategyMerge {
		t.Fatalf("DefaultStreamingStrategy() = %q, want %q", got, config.StrategyMerge)
	}
}

func TestPlanetScaleCDCColumnSupport(t *testing.T) {
	for _, discovery := range []string{"single table", "multiple tables"} {
		for _, spatial := range []bool{false, true} {
			name := discovery + "/enum set bit"
			if spatial {
				name = discovery + "/spatial rejected"
			}
			t.Run(name, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer func() { _ = db.Close() }()

				if discovery == "multiple tables" {
					mock.ExpectQuery("INFORMATION_SCHEMA\\.TABLES").
						WithArgs("app").
						WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("items"))
				}
				columns := sqlmock.NewRows([]string{
					"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH",
				}).
					AddRow("id", "bigint", "NO", nil, nil, nil).
					AddRow("status", "enum('', 'active', 'paused')", "YES", nil, nil, nil).
					AddRow("labels", "set('red', 'blue')", "YES", nil, nil, nil).
					AddRow("enabled", "bit(1)", "YES", nil, nil, nil).
					AddRow("flags", "bit(9)", "YES", nil, nil, nil).
					AddRow("wide_flags", "bit(64)", "YES", nil, nil, nil)
				support := sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"}).
					AddRow("status", "enum").AddRow("labels", "SET").
					AddRow("enabled", "bit").AddRow("flags", "bit").AddRow("wide_flags", "bit")
				if spatial {
					columns.AddRow("location", "point", "YES", nil, nil, nil)
					support.AddRow("location", "point")
				}
				mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+COLUMN_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
					WithArgs("app", "items").WillReturnRows(columns)
				mock.ExpectQuery("INFORMATION_SCHEMA\\.KEY_COLUMN_USAGE").
					WithArgs("app", "items").
					WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("id"))
				mock.ExpectQuery("(?s)SELECT\\s+COLUMN_NAME,\\s+DATA_TYPE.*INFORMATION_SCHEMA\\.COLUMNS").
					WithArgs("app", "items").WillReturnRows(support)

				src := &PlanetScaleCDCSource{db: db, keyspace: "app"}
				var tableSchema *schema.TableSchema
				if discovery == "single table" {
					var table source.SourceTable
					table, err = src.GetTable(t.Context(), source.TableRequest{Name: "items"})
					if err == nil {
						tableSchema, err = table.GetSchema(t.Context())
					}
				} else {
					var tables []source.SourceTableInfo
					tables, err = src.GetTables(t.Context())
					if err == nil {
						require.Len(t, tables, 1)
						tableSchema = tables[0].Schema
					}
				}
				require.NoError(t, mock.ExpectationsWereMet())
				if spatial {
					require.ErrorContains(t, err, "PlanetScale CDC does not support spatial (GEOMETRY)")
					require.ErrorContains(t, err, "location POINT")
					require.NotContains(t, err.Error(), "status ENUM")
					return
				}
				require.NoError(t, err)
				require.Equal(t, []string{"id"}, tableSchema.PrimaryKeys)
				var types []schema.DataType
				for _, col := range removeMySQLCDCColumns(tableSchema).Columns {
					types = append(types, col.DataType)
				}
				require.Equal(t, []schema.DataType{
					schema.TypeInt64, schema.TypeString, schema.TypeString,
					schema.TypeBinary, schema.TypeBinary, schema.TypeBinary,
				}, types)
			})
		}
	}
}

func TestPsdbCursorRoundTrip(t *testing.T) {
	state := psdbCursorState{Shards: map[string]psdbShardCursor{
		"-80": {Position: "MySQL56/abc:1-100"},
		"80-": {Position: "MySQL56/def:1-200"},
	}}

	payload, err := encodePsdbCursor(state)
	if err != nil {
		t.Fatalf("encodePsdbCursor: %v", err)
	}

	// Full _cdc_lsn round-trip through the shared LSN framing.
	lsn := formatVitessLSN(7, 0, payload)
	ord, gotPayload, ok := parseVitessLSN(lsn)
	if !ok {
		t.Fatalf("parseVitessLSN(%q) failed", lsn)
	}
	if ord != 7 {
		t.Errorf("ordinal: got %d want 7", ord)
	}

	got, err := decodePsdbCursor(gotPayload)
	if err != nil {
		t.Fatalf("decodePsdbCursor: %v", err)
	}
	if !reflect.DeepEqual(got, state) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, state)
	}

	if _, err := decodePsdbCursor("!!!not-base64!!!"); err == nil {
		t.Error("decodePsdbCursor should reject invalid payload")
	}
}

func TestPsdbStartCursorLastKnownPk(t *testing.T) {
	pk := &querypb.QueryResult{
		Fields: []*querypb.Field{{Name: "id", Type: querypb.Type_INT64}},
		Rows:   []*querypb.Row{sqltypes.RowToProto3([]sqltypes.Value{sqltypes.NewInt64(42)})},
	}
	captured := &psdbconnect.TableCursor{Keyspace: "ks", Shard: "-", Position: "ignored-during-copy", LastKnownPk: pk}

	sc, err := shardCursorFrom(captured)
	if err != nil {
		t.Fatalf("shardCursorFrom: %v", err)
	}
	if len(sc.LastKnownPk) == 0 {
		t.Fatal("expected LastKnownPk bytes to be captured")
	}

	state := psdbCursorState{Shards: map[string]psdbShardCursor{"-": sc}}
	start, err := state.startCursor("ks", "-")
	if err != nil {
		t.Fatalf("startCursor: %v", err)
	}
	// A pending snapshot must resume by primary key, with the GTID position cleared.
	if start.GetPosition() != "" {
		t.Errorf("expected empty position when LastKnownPk present, got %q", start.GetPosition())
	}
	if !proto.Equal(start.GetLastKnownPk(), pk) {
		t.Errorf("LastKnownPk round-trip mismatch:\n got  %v\n want %v", start.GetLastKnownPk(), pk)
	}

	// A position-only shard resumes from the GTID.
	posState := psdbCursorState{Shards: map[string]psdbShardCursor{"-": {Position: "MySQL56/abc:1-5"}}}
	posStart, err := posState.startCursor("ks", "-")
	if err != nil {
		t.Fatalf("startCursor(position): %v", err)
	}
	if posStart.GetPosition() != "MySQL56/abc:1-5" || posStart.GetLastKnownPk() != nil {
		t.Errorf("position-only resume mismatch: pos=%q lastPk=%v", posStart.GetPosition(), posStart.GetLastKnownPk())
	}

	// An unknown shard yields a fresh cursor.
	fresh, err := state.startCursor("ks", "missing")
	if err != nil {
		t.Fatalf("startCursor(missing): %v", err)
	}
	if fresh.GetPosition() != "" || fresh.GetLastKnownPk() != nil {
		t.Errorf("expected fresh cursor for unknown shard, got %+v", fresh)
	}
}

func TestPsdbCopyFinished(t *testing.T) {
	cases := []struct {
		name      string
		sawLastPk bool
		pos       string
		anchor    string
		hasLastPk bool
		want      bool
	}{
		{"copy start cursor", false, "MySQL56/abc:1-5", "MySQL56/abc:1-5", false, false},
		{"copy row checkpoint", true, "MySQL56/abc:1-5", "MySQL56/abc:1-5", true, false},
		{"advanced copy row checkpoint", true, "MySQL56/abc:1-6", "MySQL56/abc:1-5", true, false},
		{"copy checkpoint cleared", true, "MySQL56/abc:1-5", "MySQL56/abc:1-5", false, true},
		{"empty table position advanced", false, "MySQL56/abc:1-6", "MySQL56/abc:1-5", false, true},
		{"no position", false, "", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := psdbCopyFinished(tc.sawLastPk, tc.pos, tc.anchor, tc.hasLastPk); got != tc.want {
				t.Errorf("psdbCopyFinished = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPsdbReachedStop(t *testing.T) {
	const uuid = "3e11fa47-71ca-11e1-9e33-c80aa9429562"
	stop := "MySQL56/" + uuid + ":1-77"
	behind := "MySQL56/" + uuid + ":1-70"
	ahead := "MySQL56/" + uuid + ":1-80"

	cases := []struct {
		name                string
		copyDone, hasLastPk bool
		pos, stopPos        string
		want                bool
	}{
		// The regression case: the response carrying a shard's final change lands
		// exactly on stopPos. On an idle shard no further (empty) response arrives,
		// so the stream must stop here rather than block waiting for one.
		{"caught up exactly on final change", true, false, stop, stop, true},
		{"caught up beyond stop", true, false, ahead, stop, true},
		{"still behind stop", true, false, behind, stop, false},
		{"copy not finished yet", false, false, ahead, stop, false},
		{"pending snapshot pk", true, true, ahead, stop, false},
		{"empty position", true, false, "", stop, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := psdbReachedStop(tc.copyDone, tc.hasLastPk, tc.pos, tc.stopPos); got != tc.want {
				t.Errorf("psdbReachedStop(%v, %v, %q, %q) = %v, want %v",
					tc.copyDone, tc.hasLastPk, tc.pos, tc.stopPos, got, tc.want)
			}
		})
	}
}

func TestPsdbRewriteBufferedLSNs(t *testing.T) {
	payload, err := encodePsdbCursor(psdbCursorState{Shards: map[string]psdbShardCursor{
		"-": {Position: "MySQL56/abc:1-10"},
	}})
	if err != nil {
		t.Fatalf("encodePsdbCursor: %v", err)
	}

	buffers := map[string]*mysqlCDCChangeBuffer{
		"users": {changes: []mysqlCDCChange{
			{values: []interface{}{"kept"}, lsn: "old-0"},
			{values: []interface{}{"rewritten-1"}, lsn: "old-1"},
			{values: []interface{}{"rewritten-2"}, lsn: "old-2"},
		}},
	}

	if !psdbRewriteBufferedLSNs(buffers, "users", 1, 9, payload) {
		t.Fatal("expected buffered LSNs to be rewritten")
	}
	if buffers["users"].changes[0].lsn != "old-0" {
		t.Errorf("first change should be untouched, got %q", buffers["users"].changes[0].lsn)
	}
	if want := formatVitessLSN(9, 0, payload); buffers["users"].changes[1].lsn != want {
		t.Errorf("rewritten lsn[1]: got %q want %q", buffers["users"].changes[1].lsn, want)
	}
	if want := formatVitessLSN(9, 1, payload); buffers["users"].changes[2].lsn != want {
		t.Errorf("rewritten lsn[2]: got %q want %q", buffers["users"].changes[2].lsn, want)
	}
	if psdbRewriteBufferedLSNs(buffers, "users", len(buffers["users"].changes), 10, payload) {
		t.Error("out-of-range start should not rewrite")
	}
}

func TestPsdbStreamingTableStateCopyCompletionCheckpoint(t *testing.T) {
	pk := &querypb.QueryResult{
		Fields: []*querypb.Field{{Name: "id", Type: querypb.Type_VARCHAR}},
		Rows:   []*querypb.Row{sqltypes.RowToProto3([]sqltypes.Value{sqltypes.NewVarChar("42")})},
	}
	tableSchema := addMySQLCDCColumns(&schema.TableSchema{
		Name:        "items",
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeString, Nullable: false}},
		PrimaryKeys: []string{"id"},
	})
	state := newPsdbStreamingTableState(
		psdbCDCTarget{bareName: "items", schema: tableSchema},
		psdbCursorState{Shards: map[string]psdbShardCursor{}},
		7,
	)
	results := make(chan source.RecordBatchResult, 1)

	copyCursor := &psdbconnect.TableCursor{
		Keyspace:    "ks",
		Shard:       "-",
		Position:    "MySQL56/abc:1-10",
		LastKnownPk: pk,
	}
	checkpoint, err := state.processResponse(
		context.Background(),
		"-",
		copyCursor,
		false,
		nil,
		[]mysqlCDCChange{{values: []interface{}{"42"}}},
		true,
		100,
		results,
	)
	if err != nil {
		t.Fatalf("process copy response: %v", err)
	}
	if checkpoint == nil {
		t.Fatal("expected copy checkpoint")
	}

	finalCursor := &psdbconnect.TableCursor{
		Keyspace: "ks",
		Shard:    "-",
		Position: "MySQL56/abc:1-11",
	}
	if _, err := state.processResponse(context.Background(), "-", finalCursor, true, checkpoint, nil, false, 100, results); err != nil {
		t.Fatalf("process copy completion: %v", err)
	}

	buffer := state.buffers["items"]
	if buffer == nil || len(buffer.changes) != 2 {
		t.Fatalf("buffered changes = %+v, want original copy row plus completion checkpoint", buffer)
	}
	_, copyPayload, ok := parseVitessLSN(buffer.changes[0].lsn)
	if !ok {
		t.Fatalf("copy row LSN did not parse: %q", buffer.changes[0].lsn)
	}
	copyState, err := decodePsdbCursor(copyPayload)
	if err != nil {
		t.Fatalf("decode copy cursor: %v", err)
	}
	if len(copyState.Shards["-"].LastKnownPk) == 0 {
		t.Fatal("copy row should retain LastKnownPk for interrupted snapshot resume")
	}

	_, finalPayload, ok := parseVitessLSN(buffer.changes[1].lsn)
	if !ok {
		t.Fatalf("completion checkpoint LSN did not parse: %q", buffer.changes[1].lsn)
	}
	finalState, err := decodePsdbCursor(finalPayload)
	if err != nil {
		t.Fatalf("decode final cursor: %v", err)
	}
	finalShard := finalState.Shards["-"]
	if finalShard.Position != finalCursor.Position {
		t.Fatalf("completion checkpoint position = %q, want %q", finalShard.Position, finalCursor.Position)
	}
	if len(finalShard.LastKnownPk) != 0 {
		t.Fatal("completion checkpoint should clear LastKnownPk")
	}
}

func TestPsdbStreamingTableStateConcurrentShardFlush(t *testing.T) {
	tableSchema := addMySQLCDCColumns(&schema.TableSchema{
		Name:        "items",
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeString, Nullable: false}},
		PrimaryKeys: []string{"id"},
	})
	state := newPsdbStreamingTableState(
		psdbCDCTarget{bareName: "items", schema: tableSchema},
		psdbCursorState{Shards: map[string]psdbShardCursor{}},
		11,
	)
	results := make(chan source.RecordBatchResult, 1)

	type shardEvent struct {
		shard    string
		position string
		value    string
	}
	events := []shardEvent{
		{shard: "-80", position: "MySQL56/left:1-10", value: "left"},
		{shard: "80-", position: "MySQL56/right:1-20", value: "right"},
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, len(events))
	for _, ev := range events {
		ev := ev
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := state.processResponse(
				context.Background(),
				ev.shard,
				&psdbconnect.TableCursor{Keyspace: "ks", Shard: ev.shard, Position: ev.position},
				false,
				nil,
				[]mysqlCDCChange{{values: []interface{}{ev.value}}},
				false,
				100,
				results,
			)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("process concurrent shard response: %v", err)
		}
	}

	if err := flushPsdbStreamingTables(context.Background(), []*psdbStreamingTableState{state}, results); err != nil {
		t.Fatalf("flushPsdbStreamingTables: %v", err)
	}

	select {
	case res := <-results:
		if res.Err != nil {
			t.Fatalf("flush result error: %v", res.Err)
		}
		if res.Batch == nil {
			t.Fatal("expected flushed batch")
		}
		defer res.Batch.Release()
		if got := res.Batch.NumRows(); got != int64(len(events)) {
			t.Fatalf("flushed rows = %d, want %d", got, len(events))
		}
		lsns, ok := res.Batch.Column(1).(*array.String)
		if !ok {
			t.Fatalf("_cdc_lsn column has type %T, want *array.String", res.Batch.Column(1))
		}
		latest := lsns.Value(0)
		for i := 1; i < lsns.Len(); i++ {
			if lsn := lsns.Value(i); latest < lsn {
				latest = lsn
			}
		}
		_, payload, ok := parseVitessLSN(latest)
		if !ok {
			t.Fatalf("latest LSN did not parse: %q", latest)
		}
		flushedState, err := decodePsdbCursor(payload)
		if err != nil {
			t.Fatalf("decode latest cursor: %v", err)
		}
		for _, ev := range events {
			if got := flushedState.Shards[ev.shard].Position; got != ev.position {
				t.Fatalf("latest cursor for shard %s = %q, want %q", ev.shard, got, ev.position)
			}
		}
	default:
		t.Fatal("expected one flushed result")
	}

	if buffer := state.buffers["items"]; buffer == nil || len(buffer.changes) != 0 {
		t.Fatalf("buffer after flush = %+v, want empty", buffer)
	}
}

func TestPsdbAtLeast(t *testing.T) {
	const uuid = "3e11fa47-71ca-11e1-9e33-c80aa9429562"
	ahead := "MySQL56/" + uuid + ":1-10"
	behind := "MySQL56/" + uuid + ":1-5"

	cases := []struct {
		name      string
		pos, stop string
		want      bool
	}{
		{"equal strings", behind, behind, true},
		{"ahead of stop", ahead, behind, true},
		{"behind stop", behind, ahead, false},
		{"empty pos", "", behind, false},
		{"empty stop", ahead, "", false},
		{"unparseable", "garbage-a", "garbage-b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gtidAtLeast(tc.pos, tc.stop); got != tc.want {
				t.Errorf("gtidAtLeast(%q, %q) = %v, want %v", tc.pos, tc.stop, got, tc.want)
			}
		})
	}
}

func TestPsdbPKPositionsAndChanged(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeString},
		{Name: "name", DataType: schema.TypeString},
	}
	pks := psdbPKPositions(cols, []string{"ID"}) // case-insensitive
	if !reflect.DeepEqual(pks, []int{0}) {
		t.Fatalf("psdbPKPositions: got %v want [0]", pks)
	}

	if !psdbPKChanged([]interface{}{"1", "a"}, []interface{}{"2", "a"}, pks) {
		t.Error("expected PK change detected for differing id")
	}
	if psdbPKChanged([]interface{}{"1", "a"}, []interface{}{"1", "b"}, pks) {
		t.Error("did not expect PK change for same id")
	}
}

func TestDecodePsdbChanges(t *testing.T) {
	sourceCols := []schema.Column{
		{Name: "id", DataType: schema.TypeString},
		{Name: "name", DataType: schema.TypeString},
	}
	fullFields := []*querypb.Field{
		{Name: "id", Type: querypb.Type_INT64},
		{Name: "name", Type: querypb.Type_VARCHAR},
	}
	fullRow := func(id int64, name string) *querypb.QueryResult {
		return &querypb.QueryResult{
			Fields: fullFields,
			Rows:   []*querypb.Row{sqltypes.RowToProto3([]sqltypes.Value{sqltypes.NewInt64(id), sqltypes.NewVarChar(name)})},
		}
	}

	resp := &psdbconnect.SyncResponse{
		Result: []*querypb.QueryResult{fullRow(1, "alice")},
		Updates: []*psdbconnect.UpdatedRow{
			{Before: fullRow(1, "alice"), After: fullRow(2, "alice")},  // PK change -> delete + insert
			{Before: fullRow(3, "carol"), After: fullRow(3, "carol2")}, // non-PK change -> insert only
		},
		Deletes: []*psdbconnect.DeletedRow{
			// PlanetScale deletes carry only primary keys.
			{Result: &querypb.QueryResult{
				Fields: []*querypb.Field{{Name: "id", Type: querypb.Type_INT64}},
				Rows:   []*querypb.Row{sqltypes.RowToProto3([]sqltypes.Value{sqltypes.NewInt64(4)})},
			}},
		},
	}

	changes, err := decodePsdbChanges(resp, sourceCols, []int{0})
	if err != nil {
		t.Fatalf("decodePsdbChanges: %v", err)
	}

	want := []mysqlCDCChange{
		{values: []interface{}{"1", "alice"}, deleted: false},  // insert
		{values: []interface{}{"1", "alice"}, deleted: true},   // PK-change before -> tombstone
		{values: []interface{}{"2", "alice"}, deleted: false},  // PK-change after -> upsert
		{values: []interface{}{"3", "carol2"}, deleted: false}, // non-PK update -> upsert
		{values: []interface{}{"4", nil}, deleted: true},       // delete (PK only, non-PK NULL)
	}
	if len(changes) != len(want) {
		t.Fatalf("change count: got %d want %d (%+v)", len(changes), len(want), changes)
	}
	for i, w := range want {
		if changes[i].deleted != w.deleted {
			t.Errorf("change %d deleted: got %v want %v", i, changes[i].deleted, w.deleted)
		}
		if !reflect.DeepEqual(changes[i].values, w.values) {
			t.Errorf("change %d values: got %#v want %#v", i, changes[i].values, w.values)
		}
	}
}

func TestDecodePsdbChangesEnumSetBit(t *testing.T) {
	sourceCols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "status", DataType: schema.TypeString},
		{Name: "labels", DataType: schema.TypeString},
		{Name: "enabled", DataType: schema.TypeBinary},
		{Name: "flags", DataType: schema.TypeBinary},
		{Name: "wide_flags", DataType: schema.TypeBinary},
	}
	fields := []*querypb.Field{
		{Name: "id", Type: querypb.Type_INT64},
		{Name: "status", Type: querypb.Type_ENUM},
		{Name: "labels", Type: querypb.Type_SET},
		{Name: "enabled", Type: querypb.Type_BIT},
		{Name: "flags", Type: querypb.Type_BIT},
		{Name: "wide_flags", Type: querypb.Type_BIT},
	}
	row := func(id int64, status, labels string, enabled, flags, wideFlags []byte) *querypb.Row {
		return sqltypes.RowToProto3([]sqltypes.Value{
			sqltypes.NewInt64(id),
			sqltypes.MakeTrusted(querypb.Type_ENUM, []byte(status)),
			sqltypes.MakeTrusted(querypb.Type_SET, []byte(labels)),
			sqltypes.MakeTrusted(querypb.Type_BIT, enabled),
			sqltypes.MakeTrusted(querypb.Type_BIT, flags),
			sqltypes.MakeTrusted(querypb.Type_BIT, wideFlags),
		})
	}
	maxBits := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	zeroBits := make([]byte, 8)
	initial := row(1, "active", "red,blue", []byte{1}, []byte{1, 0xff}, maxBits)
	updated := row(4, "paused", "blue", []byte{0}, []byte{0, 1}, zeroBits)
	resp := &psdbconnect.SyncResponse{
		Result: []*querypb.QueryResult{{
			Fields: fields,
			Rows: []*querypb.Row{
				initial,
				row(2, "", "", []byte{0}, []byte{0, 0}, zeroBits),
				sqltypes.RowToProto3([]sqltypes.Value{
					sqltypes.NewInt64(3), sqltypes.NULL, sqltypes.NULL, sqltypes.NULL, sqltypes.NULL, sqltypes.NULL,
				}),
			},
		}},
		Updates: []*psdbconnect.UpdatedRow{{
			Before: &querypb.QueryResult{Fields: fields, Rows: []*querypb.Row{initial}},
			After:  &querypb.QueryResult{Fields: fields, Rows: []*querypb.Row{updated}},
		}},
		Deletes: []*psdbconnect.DeletedRow{{Result: &querypb.QueryResult{
			Fields: fields[:1],
			Rows:   []*querypb.Row{sqltypes.RowToProto3([]sqltypes.Value{sqltypes.NewInt64(2)})},
		}}},
	}

	changes, err := decodePsdbChanges(resp, sourceCols, []int{0})
	require.NoError(t, err)
	require.Equal(t, []mysqlCDCChange{
		{values: []interface{}{"1", "active", "red,blue", []byte{1}, []byte{1, 0xff}, maxBits}},
		{values: []interface{}{"2", "", "", []byte{0}, []byte{0, 0}, zeroBits}},
		{values: []interface{}{"3", nil, nil, nil, nil, nil}},
		{values: []interface{}{"1", "active", "red,blue", []byte{1}, []byte{1, 0xff}, maxBits}, deleted: true},
		{values: []interface{}{"4", "paused", "blue", []byte{0}, []byte{0, 1}, zeroBits}},
		{values: []interface{}{"2", nil, nil, nil, nil, nil}, deleted: true},
	}, changes)
}

func TestPsdbResultRowsCopiesBitValues(t *testing.T) {
	qr := &querypb.QueryResult{
		Fields: []*querypb.Field{
			{Name: "enabled", Type: querypb.Type_BIT},
			{Name: "flags", Type: querypb.Type_BIT},
			{Name: "wide_flags", Type: querypb.Type_BIT},
		},
		Rows: []*querypb.Row{sqltypes.RowToProto3([]sqltypes.Value{
			sqltypes.MakeTrusted(querypb.Type_BIT, []byte{1}),
			sqltypes.MakeTrusted(querypb.Type_BIT, []byte{1, 0xff}),
			sqltypes.MakeTrusted(querypb.Type_BIT, []byte{0x80, 0, 0, 0, 0, 0, 0, 1}),
		})},
	}
	rows, err := psdbResultRows(qr, []schema.Column{
		{Name: "enabled", DataType: schema.TypeBinary},
		{Name: "flags", DataType: schema.TypeBinary},
		{Name: "wide_flags", DataType: schema.TypeBinary},
	})
	require.NoError(t, err)
	clear(qr.Rows[0].Values)
	require.Equal(t, [][]interface{}{{[]byte{1}, []byte{1, 0xff}, []byte{0x80, 0, 0, 0, 0, 0, 0, 1}}}, rows)
}
