package mysql

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"google.golang.org/protobuf/proto"
	"vitess.io/vitess/go/sqltypes"
	binlogdatapb "vitess.io/vitess/go/vt/proto/binlogdata"
	querypb "vitess.io/vitess/go/vt/proto/query"
)

func TestVitessLSNRoundTripAndOrdering(t *testing.T) {
	payload, err := encodeVitessVGtid(&binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
		{Keyspace: "vtdb", Shard: "0", Gtid: "MySQL56/abc:1-100"},
	}})
	if err != nil {
		t.Fatalf("encodeVitessVGtid: %v", err)
	}

	lsn := formatVitessLSN(5, 3, payload)

	gotOrd, gotPayload, ok := parseVitessLSN(lsn)
	if !ok {
		t.Fatalf("parseVitessLSN(%q) failed", lsn)
	}
	if gotOrd != 5 {
		t.Errorf("ordinal: got %d, want 5", gotOrd)
	}
	if gotPayload != payload {
		t.Errorf("payload mismatch: got %q want %q", gotPayload, payload)
	}

	// Plain text comparison must order by ordinal, so MAX(_cdc_lsn) picks the latest.
	older := formatVitessLSN(5, 0, payload)
	newer := formatVitessLSN(12, 0, payload)
	if newer <= older {
		t.Errorf("expected ordinal 12 LSN to sort after ordinal 5: %q vs %q", newer, older)
	}
}

func TestParseVitessLSNRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "not-an-lsn", "123:456:payload", "abc:000000:payload"} {
		if _, _, ok := parseVitessLSN(bad); ok {
			t.Errorf("parseVitessLSN(%q) should have failed", bad)
		}
	}
}

func TestVitessVGtidEncodeDecodeRoundTrip(t *testing.T) {
	want := &binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
		{Keyspace: "vtdb", Shard: "-80", Gtid: "MySQL56/abc:1-100"},
		{Keyspace: "vtdb", Shard: "80-", Gtid: "MySQL56/def:1-200"},
	}}

	payload, err := encodeVitessVGtid(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeVitessVGtid(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !proto.Equal(want, got) {
		t.Errorf("round-trip mismatch:\n want %v\n got  %v", want, got)
	}

	if _, err := decodeVitessVGtid("!!!not-base64!!!"); err == nil {
		t.Error("decodeVitessVGtid should reject invalid payload")
	}
}

func TestVitessGRPCTarget(t *testing.T) {
	cases := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{"port only defaults host", "vitess+cdc://root@db.example:3307/ks?grpc_port=15991", "db.example:15991", false},
		{"explicit grpc_host", "vitess+cdc://root@db.example:3307/ks?grpc_port=15991&grpc_host=vtgate.internal", "vtgate.internal:15991", false},
		{"missing grpc_port", "vitess+cdc://root@db.example:3307/ks", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := vitessGRPCTarget(tc.uri, "db.example")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The MySQL DSN must never carry the Vitess-only gRPC params, otherwise the
// go-sql-driver rejects them as unknown. The tls param, by contrast, is a real
// driver param and must be retained.
func TestMySQLCDCURIStripsGRPCParams(t *testing.T) {
	_, normalizedURI, _, err := parseMySQLCDCURI("vitess+cdc://root@db.example:3307/ks?grpc_port=15991&grpc_host=vtgate&grpc_tls=true&tls=true")
	if err != nil {
		t.Fatalf("parseMySQLCDCURI: %v", err)
	}
	dsn, _, err := uriToDSN(normalizedURI)
	if err != nil {
		t.Fatalf("uriToDSN: %v", err)
	}
	if strings.Contains(dsn, "grpc_port") || strings.Contains(dsn, "grpc_host") || strings.Contains(dsn, "grpc_tls") {
		t.Errorf("DSN must not contain gRPC params: %q", dsn)
	}
	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("DSN must retain the MySQL tls param: %q", dsn)
	}
}

// vitessGRPCTLSCredentials must turn the user-facing tls/grpc_tls knobs into the
// right transport security: a single tls=true secures both connections, while
// grpc_tls overrides the gRPC side independently. A tls value only the MySQL
// driver understands (preferred, custom config name) must be an explicit error,
// never a silent plaintext downgrade.
func TestVitessGRPCTLSCredentials(t *testing.T) {
	cases := []struct {
		name    string
		uri     string
		wantTLS bool
		wantErr bool
	}{
		{"no params is plaintext", "vitess+cdc://root@h:3307/ks?grpc_port=15991", false, false},
		{"tls=true is inherited", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=true", true, false},
		{"tls=skip-verify is inherited", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=skip-verify", true, false},
		{"tls=false stays plaintext", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=false", false, false},
		{"tls=preferred requires explicit grpc_tls", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=preferred", false, true},
		{"tls=customCA requires explicit grpc_tls", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=mycustomca", false, true},
		{"tls=customCA with explicit grpc_tls=true", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=mycustomca&grpc_tls=true", true, false},
		{"grpc_tls=true overrides", "vitess+cdc://root@h:3307/ks?grpc_port=15991&grpc_tls=true", true, false},
		{"grpc_tls=false overrides tls=true", "vitess+cdc://root@h:3307/ks?grpc_port=15991&tls=true&grpc_tls=false", false, false},
		{"grpc_tls=skip-verify overrides", "vitess+cdc://root@h:3307/ks?grpc_port=15991&grpc_tls=skip-verify", true, false},
		{"invalid grpc_tls errors", "vitess+cdc://root@h:3307/ks?grpc_port=15991&grpc_tls=maybe", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			creds, err := vitessGRPCTLSCredentials(tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got credentials %v", creds.Info().SecurityProtocol)
				}
				if !strings.Contains(err.Error(), "grpc_tls") {
					t.Errorf("error should point at grpc_tls: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotTLS := creds.Info().SecurityProtocol == "tls"
			if gotTLS != tc.wantTLS {
				t.Errorf("got SecurityProtocol=%q (tls=%v), want tls=%v", creds.Info().SecurityProtocol, gotTLS, tc.wantTLS)
			}
		})
	}
}

// planVitessStart must partition targets by cursor availability instead of
// discarding valid cursors when some tables lack one: a full re-copy would
// silently miss deletes that happened since those cursors were written.
func TestPlanVitessStart(t *testing.T) {
	vgtidAt := func(gtid string) string {
		payload, err := encodeVitessVGtid(&binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
			{Keyspace: "vtdb", Shard: "-", Gtid: gtid},
		}})
		if err != nil {
			t.Fatalf("encodeVitessVGtid: %v", err)
		}
		return payload
	}
	targets := []vitessCDCTarget{{bareName: "old1"}, {bareName: "old2"}, {bareName: "brandnew"}}

	t.Run("mixed cursors partition into resume and fresh", func(t *testing.T) {
		plan, err := planVitessStart(targets, map[string]string{
			"old1": formatVitessLSN(3, 0, vgtidAt("MySQL56/abc:1-30")),
			"old2": formatVitessLSN(7, 0, vgtidAt("MySQL56/abc:1-70")),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.resume) != 2 || len(plan.fresh) != 1 || plan.fresh[0].bareName != "brandnew" {
			t.Fatalf("partition mismatch: resume=%+v fresh=%+v", plan.resume, plan.fresh)
		}
		if plan.ordinal != 8 {
			t.Errorf("ordinal should seed past the max stored ordinal: got %d want 8", plan.ordinal)
		}
		// The resume stream must start from the OLDEST cursor so the most-behind
		// table misses nothing; re-delivery to newer tables is idempotent.
		if got := plan.resumeVGtid.ShardGtids[0].Gtid; got != "MySQL56/abc:1-30" {
			t.Errorf("resume VGTID should be the oldest cursor, got %q", got)
		}
	})

	t.Run("all fresh", func(t *testing.T) {
		plan, err := planVitessStart(targets, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.fresh) != 3 || len(plan.resume) != 0 || plan.ordinal != 0 {
			t.Fatalf("expected all-fresh plan, got %+v", plan)
		}
	})

	t.Run("invalid cursor errors with full-refresh guidance", func(t *testing.T) {
		_, err := planVitessStart(targets, map[string]string{"old1": "garbage"})
		if err == nil || !strings.Contains(err.Error(), "--full-refresh") {
			t.Fatalf("expected full-refresh guidance, got %v", err)
		}
	})
}

// vitessPendingCopyShards: a fresh run copies on every shard; a resume run only
// owes a copy on shards whose stored VGTID carries TablePKs (interrupted copy).
func TestVitessPendingCopyShards(t *testing.T) {
	shards := []string{"-80", "80-"}

	fresh := vitessPendingCopyShards(freshVitessVGtid("vtdb", shards), false, shards)
	if len(fresh) != 2 || !fresh["-80"] || !fresh["80-"] {
		t.Errorf("fresh run should owe a copy on all shards: %v", fresh)
	}

	clean := vitessPendingCopyShards(&binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
		{Keyspace: "vtdb", Shard: "-80", Gtid: "MySQL56/a:1-5"},
		{Keyspace: "vtdb", Shard: "80-", Gtid: "MySQL56/b:1-5"},
	}}, true, shards)
	if len(clean) != 0 {
		t.Errorf("clean resume should owe no copy: %v", clean)
	}

	interrupted := vitessPendingCopyShards(&binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
		{Keyspace: "vtdb", Shard: "-80", Gtid: "MySQL56/a:1-5"},
		{Keyspace: "vtdb", Shard: "80-", Gtid: "MySQL56/b:1-5", TablePKs: []*binlogdatapb.TableLastPK{{TableName: "items"}}},
	}}, true, shards)
	if len(interrupted) != 1 || !interrupted["80-"] {
		t.Errorf("resume with TablePKs should owe a copy on that shard: %v", interrupted)
	}
}

// vitessCaughtUp gates batch-mode termination: every shard must have reached the
// stop boundary captured at stream start.
func TestVitessCaughtUp(t *testing.T) {
	const uuid = "3e11fa47-71ca-11e1-9e33-c80aa9429562"
	stop := map[string]string{
		"-80": "MySQL56/" + uuid + ":1-50",
		"80-": "MySQL56/" + uuid + ":1-70",
	}
	vgtid := func(a, b string) *binlogdatapb.VGtid {
		return &binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
			{Keyspace: "vtdb", Shard: "-80", Gtid: a},
			{Keyspace: "vtdb", Shard: "80-", Gtid: b},
		}}
	}

	if vitessCaughtUp(nil, stop, "vtdb") {
		t.Error("nil VGTID is never caught up")
	}
	if vitessCaughtUp(vgtid("", ""), stop, "vtdb") {
		t.Error("fresh (empty) positions are not caught up")
	}
	if vitessCaughtUp(vgtid("MySQL56/"+uuid+":1-50", "MySQL56/"+uuid+":1-60"), stop, "vtdb") {
		t.Error("one shard behind must not report caught up")
	}
	if !vitessCaughtUp(vgtid("MySQL56/"+uuid+":1-50", "MySQL56/"+uuid+":1-70"), stop, "vtdb") {
		t.Error("all shards exactly at stop should be caught up")
	}
	if !vitessCaughtUp(vgtid("MySQL56/"+uuid+":1-99", "MySQL56/"+uuid+":1-99"), stop, "vtdb") {
		t.Error("all shards beyond stop should be caught up")
	}

	// ShardGtids from other keyspaces must not satisfy this keyspace's boundary.
	other := &binlogdatapb.VGtid{ShardGtids: []*binlogdatapb.ShardGtid{
		{Keyspace: "otherks", Shard: "-80", Gtid: "MySQL56/" + uuid + ":1-99"},
		{Keyspace: "otherks", Shard: "80-", Gtid: "MySQL56/" + uuid + ":1-99"},
	}}
	if vitessCaughtUp(other, stop, "vtdb") {
		t.Error("positions from another keyspace must not count")
	}

	if vitessCaughtUp(vgtid("MySQL56/"+uuid+":1-99", "MySQL56/"+uuid+":1-99"), map[string]string{}, "vtdb") {
		t.Error("an empty stop boundary must never report caught up")
	}
}

func TestVitessDecodeRowChanges(t *testing.T) {
	base := &schema.TableSchema{
		Name:   "items",
		Schema: "vtdb",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32, IsPrimaryKey: true},
			{Name: "name", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}
	out := addMySQLCDCColumns(base)
	out.PrimaryKeys = []string{"id"}

	info := &vitessFieldInfo{
		fields: []*querypb.Field{
			{Name: "id", Type: querypb.Type_INT32},
			{Name: "name", Type: querypb.Type_VARCHAR},
		},
		idxByName: map[string]int{"id": 0, "name": 1},
	}
	row := func(id int32, name string) *querypb.Row {
		return sqltypes.RowToProto3([]sqltypes.Value{sqltypes.NewInt32(id), sqltypes.NewVarChar(name)})
	}

	t.Run("insert", func(t *testing.T) {
		changes, err := vitessDecodeRowChanges("items", &binlogdatapb.RowEvent{
			TableName:  "items",
			RowChanges: []*binlogdatapb.RowChange{{After: row(1, "a")}},
		}, out, info)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 1 || changes[0].deleted {
			t.Fatalf("expected 1 non-deleted change, got %+v", changes)
		}
		if changes[0].values[0] != "1" || changes[0].values[1] != "a" {
			t.Errorf("unexpected values: %+v", changes[0].values)
		}
	})

	t.Run("delete", func(t *testing.T) {
		changes, err := vitessDecodeRowChanges("items", &binlogdatapb.RowEvent{
			TableName:  "items",
			RowChanges: []*binlogdatapb.RowChange{{Before: row(2, "b")}},
		}, out, info)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 1 || !changes[0].deleted {
			t.Fatalf("expected 1 deleted change, got %+v", changes)
		}
	})

	t.Run("update without pk change", func(t *testing.T) {
		changes, err := vitessDecodeRowChanges("items", &binlogdatapb.RowEvent{
			TableName:  "items",
			RowChanges: []*binlogdatapb.RowChange{{Before: row(1, "a"), After: row(1, "b")}},
		}, out, info)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 1 || changes[0].deleted || changes[0].values[1] != "b" {
			t.Fatalf("expected 1 upsert with new value, got %+v", changes)
		}
	})

	t.Run("update with pk change emits tombstone and upsert", func(t *testing.T) {
		changes, err := vitessDecodeRowChanges("items", &binlogdatapb.RowEvent{
			TableName:  "items",
			RowChanges: []*binlogdatapb.RowChange{{Before: row(1, "a"), After: row(2, "a")}},
		}, out, info)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 2 {
			t.Fatalf("expected tombstone + upsert, got %+v", changes)
		}
		if !changes[0].deleted || changes[0].values[0] != "1" {
			t.Errorf("first change should be tombstone of old PK: %+v", changes[0])
		}
		if changes[1].deleted || changes[1].values[0] != "2" {
			t.Errorf("second change should be upsert of new PK: %+v", changes[1])
		}
	})
}
