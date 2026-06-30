package mysql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	psdbconnect "github.com/bruin-data/ingestr/pkg/source/mysql/internal/psdbconnect"
	"google.golang.org/protobuf/proto"
	"vitess.io/vitess/go/sqltypes"
	querypb "vitess.io/vitess/go/vt/proto/query"
)

func TestParseMySQLCDCURIPlanetScaleParams(t *testing.T) {
	uri := "mysql+cdc://user:pass@abc.connect.psdb.cloud:3306/mydb?tls=true&psdb_token_name=tok-name&psdb_token=tok-secret&cdc_backend=planetscale"

	cfg, normalized, connInfo, err := parseMySQLCDCURI(uri)
	if err != nil {
		t.Fatalf("parseMySQLCDCURI: %v", err)
	}

	if cfg.PSDBToken != "tok-secret" {
		t.Errorf("PSDBToken: got %q want %q", cfg.PSDBToken, "tok-secret")
	}
	if cfg.PSDBTokenName != "tok-name" {
		t.Errorf("PSDBTokenName: got %q want %q", cfg.PSDBTokenName, "tok-name")
	}
	if cfg.CDCBackend != "planetscale" {
		t.Errorf("CDCBackend: got %q want %q", cfg.CDCBackend, "planetscale")
	}
	if connInfo.Host != "abc.connect.psdb.cloud" {
		t.Errorf("Host: got %q", connInfo.Host)
	}
	if connInfo.Database != "mydb" {
		t.Errorf("Database: got %q", connInfo.Database)
	}

	// PlanetScale-only params must be stripped from the MySQL URI (the driver
	// rejects unknown params), but tls must survive for the schema connection.
	for _, leaked := range []string{"psdb_token", "psdb_token_name", "cdc_backend"} {
		if strings.Contains(normalized, leaked) {
			t.Errorf("normalized URI must not contain %q: %s", leaked, normalized)
		}
	}
	if !strings.Contains(normalized, "tls=true") {
		t.Errorf("normalized URI must retain tls=true: %s", normalized)
	}
}

func TestUsePlanetScaleCDC(t *testing.T) {
	cases := []struct {
		name string
		cfg  MySQLCDCConfig
		host string
		want bool
	}{
		{"service token", MySQLCDCConfig{PSDBToken: "x", PSDBTokenName: "y"}, "db.example", true},
		{"token name only", MySQLCDCConfig{PSDBTokenName: "y"}, "db.example", true},
		{"psdb.cloud host", MySQLCDCConfig{}, "abc.connect.psdb.cloud", true},
		{"psdb.cloud host uppercase", MySQLCDCConfig{}, "ABC.CONNECT.PSDB.CLOUD", true},
		{"backend override on", MySQLCDCConfig{CDCBackend: "planetscale"}, "db.example", true},
		{"backend override off wins over token", MySQLCDCConfig{CDCBackend: "vstream", PSDBToken: "x"}, "abc.psdb.cloud", false},
		{"plain mysql", MySQLCDCConfig{}, "db.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := usePlanetScaleCDC(tc.cfg, tc.host); got != tc.want {
				t.Errorf("usePlanetScaleCDC(%+v, %q) = %v, want %v", tc.cfg, tc.host, got, tc.want)
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

func TestPsdbCopyComplete(t *testing.T) {
	cases := []struct {
		name string
		cur  *psdbconnect.TableCursor
		want bool
	}{
		{"nil", nil, false},
		{"copying (last_known_pk set)", &psdbconnect.TableCursor{LastKnownPk: &querypb.QueryResult{}}, false},
		{"empty position", &psdbconnect.TableCursor{Position: ""}, false},
		{"streaming", &psdbconnect.TableCursor{Position: "MySQL56/abc:1-5"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := psdbCopyComplete(tc.cur); got != tc.want {
				t.Errorf("psdbCopyComplete = %v, want %v", got, tc.want)
			}
		})
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
