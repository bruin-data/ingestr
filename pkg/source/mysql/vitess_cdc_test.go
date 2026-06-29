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
		{"port only defaults host", "mysql+cdc://root@db.example:3307/ks?grpc_port=15991", "db.example:15991", false},
		{"explicit grpc_host", "mysql+cdc://root@db.example:3307/ks?grpc_port=15991&grpc_host=vtgate.internal", "vtgate.internal:15991", false},
		{"missing grpc_port", "mysql+cdc://root@db.example:3307/ks", "", true},
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
// go-sql-driver rejects them as unknown.
func TestMySQLCDCURIStripsGRPCParams(t *testing.T) {
	_, normalizedURI, _, err := parseMySQLCDCURI("mysql+cdc://root@db.example:3307/ks?grpc_port=15991&grpc_host=vtgate")
	if err != nil {
		t.Fatalf("parseMySQLCDCURI: %v", err)
	}
	dsn, _, err := uriToDSN(normalizedURI)
	if err != nil {
		t.Fatalf("uriToDSN: %v", err)
	}
	if strings.Contains(dsn, "grpc_port") || strings.Contains(dsn, "grpc_host") {
		t.Errorf("DSN must not contain gRPC params: %q", dsn)
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
