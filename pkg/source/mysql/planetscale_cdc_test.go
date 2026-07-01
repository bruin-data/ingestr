package mysql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/schema"
	psdbconnect "github.com/bruin-data/ingestr/pkg/source/mysql/internal/psdbconnect"
	"google.golang.org/protobuf/proto"
	"vitess.io/vitess/go/sqltypes"
	querypb "vitess.io/vitess/go/vt/proto/query"
)

func TestParseMySQLCDCURIPlanetScaleParams(t *testing.T) {
	uri := "planetscale+cdc://user:pscale_pw_secret@abc.connect.psdb.cloud:3306/mydb"

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

	// The +cdc suffix is stripped, leaving the planetscale scheme so uriToDSN can
	// auto-enable TLS for the underlying MySQL connection.
	if !strings.HasPrefix(normalized, "planetscale://") {
		t.Errorf("normalized URI must keep the planetscale scheme: %s", normalized)
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
		{"planetscale", (*VitessSource)(nil)},
		{"mysql+cdc", (*MySQLCDCSource)(nil)},
		{"vitess+cdc", (*VitessCDCSource)(nil)},
		{"planetscale+cdc", (*PlanetScaleCDCSource)(nil)},
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
			if got := psdbAtLeast(tc.pos, tc.stop); got != tc.want {
				t.Errorf("psdbAtLeast(%q, %q) = %v, want %v", tc.pos, tc.stop, got, tc.want)
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
