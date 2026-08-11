package postgres_cdc

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func be64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func mustConvertBinary(t *testing.T, data []byte, col schema.Column, oid uint32) interface{} {
	t.Helper()
	v, err := convertBinaryValue(data, col, oid, pgtype.NewMap())
	require.NoError(t, err)
	return v
}

// Binary decoding must produce the exact Go shapes convertTextValue produces,
// so Arrow materialization is identical whether the stream runs in text or
// binary mode.
func TestConvertBinaryValueMatchesTextShapes(t *testing.T) {
	m := pgtype.NewMap()

	t.Run("bool", func(t *testing.T) {
		assert.Equal(t, true, mustConvertBinary(t, []byte{1}, schema.Column{DataType: schema.TypeBoolean}, oidBool))
		assert.Equal(t, false, mustConvertBinary(t, []byte{0}, schema.Column{DataType: schema.TypeBoolean}, oidBool))
	})

	t.Run("integers", func(t *testing.T) {
		assert.Equal(t, int16(-3), mustConvertBinary(t, be16(uint16(0xFFFD)), schema.Column{DataType: schema.TypeInt16}, oidInt2))
		assert.Equal(t, int32(12345), mustConvertBinary(t, be32(12345), schema.Column{DataType: schema.TypeInt32}, oidInt4))
		assert.Equal(t, int64(123456789012), mustConvertBinary(t, be64(123456789012), schema.Column{DataType: schema.TypeInt64}, oidInt8))
	})

	t.Run("floats", func(t *testing.T) {
		assert.Equal(t, float32(3.14), mustConvertBinary(t, be32(math.Float32bits(3.14)), schema.Column{DataType: schema.TypeFloat32}, oidFloat4))
		assert.Equal(t, 3.14159265359, mustConvertBinary(t, be64(math.Float64bits(3.14159265359)), schema.Column{DataType: schema.TypeFloat64}, oidFloat8))
	})

	t.Run("timestamp equals text-parsed instant", func(t *testing.T) {
		want := time.Date(2024, 1, 15, 10, 30, 45, 123456000, time.UTC)
		micros := want.Sub(pgEpoch).Microseconds()
		got := mustConvertBinary(t, be64(uint64(micros)), schema.Column{DataType: schema.TypeTimestamp}, oidTimestamp)
		require.IsType(t, time.Time{}, got)
		assert.True(t, want.Equal(got.(time.Time)))

		textGot, err := convertTextValue("2024-01-15 10:30:45.123456", schema.Column{DataType: schema.TypeTimestamp})
		require.NoError(t, err)
		assert.Equal(t, textGot.(time.Time).UnixMicro(), got.(time.Time).UnixMicro())
	})

	t.Run("timestamp infinity is rejected", func(t *testing.T) {
		got, err := convertBinaryValue(be64(uint64(math.MaxInt64)), schema.Column{DataType: schema.TypeTimestampTZ}, oidTimestamptz, pgtype.NewMap())
		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("date infinity is rejected", func(t *testing.T) {
		got, err := convertBinaryValue(be32(uint32(math.MaxInt32)), schema.Column{DataType: schema.TypeDate}, oidDate, pgtype.NewMap())
		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("far future timestamp does not overflow duration", func(t *testing.T) {
		want := time.Date(2500, 1, 15, 10, 30, 45, 123456000, time.UTC)
		micros := (want.Unix()-pgEpoch.Unix())*1_000_000 + int64(want.Nanosecond()/1_000)
		got := mustConvertBinary(t, be64(uint64(micros)), schema.Column{DataType: schema.TypeTimestampTZ}, oidTimestamptz)
		assert.Equal(t, want, got)
	})

	t.Run("date", func(t *testing.T) {
		days := uint32(8780) // 2024-01-15 is 8780 days after 2000-01-01
		got := mustConvertBinary(t, be32(days), schema.Column{DataType: schema.TypeDate}, oidDate)
		textGot, err := convertTextValue("2024-01-15", schema.Column{DataType: schema.TypeDate})
		require.NoError(t, err)
		assert.Equal(t, textGot, got)
	})

	t.Run("time of day", func(t *testing.T) {
		micros := (10*3600 + 30*60 + 45) * int64(time.Second/time.Microsecond)
		got := mustConvertBinary(t, be64(uint64(micros)), schema.Column{DataType: schema.TypeTime}, oidTime)
		textGot, err := convertTextValue("10:30:45", schema.Column{DataType: schema.TypeTime})
		require.NoError(t, err)
		assert.Equal(t, textGot, got)
	})

	t.Run("numeric decodes to string like the text path", func(t *testing.T) {
		var buf []byte
		want := "123.45"
		var n pgtype.Numeric
		require.NoError(t, n.Scan(want))
		enc, err := m.Encode(pgtype.NumericOID, pgtype.BinaryFormatCode, n, buf)
		require.NoError(t, err)
		got := mustConvertBinary(t, enc, schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2}, oidNumeric)
		assert.Equal(t, want, got)
	})

	t.Run("text-ish strings", func(t *testing.T) {
		assert.Equal(t, "hello", mustConvertBinary(t, []byte("hello"), schema.Column{DataType: schema.TypeString}, oidText))
		assert.Equal(t, "v", mustConvertBinary(t, []byte("v"), schema.Column{DataType: schema.TypeString}, oidVarchar))
	})

	t.Run("uuid formats canonically", func(t *testing.T) {
		raw := []byte{0x55, 0x0e, 0x84, 0x00, 0xe2, 0x9b, 0x41, 0xd4, 0xa7, 0x16, 0x44, 0x66, 0x55, 0x44, 0x00, 0x00}
		want := "550e8400-e29b-41d4-a716-446655440000"
		assert.Equal(t, want, mustConvertBinary(t, raw, schema.Column{DataType: schema.TypeUUID}, oidUUID))
		// uuid columns mapped to plain string schemas decode the same way.
		assert.Equal(t, want, mustConvertBinary(t, raw, schema.Column{DataType: schema.TypeString}, oidUUID))
	})

	t.Run("json and jsonb", func(t *testing.T) {
		assert.Equal(t, `{"a":1}`, mustConvertBinary(t, []byte(`{"a":1}`), schema.Column{DataType: schema.TypeJSON}, oidJSON))
		assert.Equal(t, `{"a":1}`, mustConvertBinary(t, append([]byte{1}, []byte(`{"a":1}`)...), schema.Column{DataType: schema.TypeJSON}, oidJSONB))
	})

	t.Run("bytea keeps raw bytes copied", func(t *testing.T) {
		src := []byte{0xDE, 0xAD}
		got := mustConvertBinary(t, src, schema.Column{DataType: schema.TypeBinary}, oidBytea)
		require.IsType(t, []byte{}, got)
		src[0] = 0x00
		assert.Equal(t, []byte{0xDE, 0xAD}, got, "decoded value must not alias the wire buffer")
	})

	t.Run("int array", func(t *testing.T) {
		// {1, NULL, 3} as binary: ndim=1, hasnull=1, elem oid, dim(3,1), elements.
		data := be32(1)
		data = append(data, be32(1)...)
		data = append(data, be32(oidInt4)...)
		data = append(data, be32(3)...) // dim size
		data = append(data, be32(1)...) // lower bound
		data = append(data, be32(4)...)
		data = append(data, be32(1)...)
		data = append(data, be32(0xFFFFFFFF)...) // NULL
		data = append(data, be32(4)...)
		data = append(data, be32(3)...)

		got := mustConvertBinary(t, data, schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt32}, 1007)
		assert.Equal(t, []interface{}{int32(1), nil, int32(3)}, got)

		textGot, err := convertTextValue("{1,NULL,3}", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt32})
		require.NoError(t, err)
		assert.Equal(t, textGot, got)
	})

	t.Run("empty array", func(t *testing.T) {
		data := be32(0) // ndim = 0
		data = append(data, be32(0)...)
		data = append(data, be32(oidInt4)...)
		got := mustConvertBinary(t, data, schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt32}, 1007)
		assert.Equal(t, []interface{}{}, got)
	})

	t.Run("unsupported oid fails fast with guidance", func(t *testing.T) {
		_, err := convertBinaryValue([]byte{0, 0}, schema.Column{Name: "iv", DataType: schema.TypeInterval}, 1186, m)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "binary=true")

		_, err = convertBinaryValue([]byte("label"), schema.Column{Name: "status", DataType: schema.TypeString}, 99999, m)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "binary=true")
	})
}

func TestBuildPluginArgs(t *testing.T) {
	cfg := CDCConfig{Publication: "pub"}

	t.Run("pre-14 server stays on v1", func(t *testing.T) {
		args := buildPluginArgs(cfg, 130000, true)
		assert.Equal(t, []string{"proto_version '1'", "publication_names 'pub'"}, args)
	})

	t.Run("14+ multi-table gets v2 streaming", func(t *testing.T) {
		args := buildPluginArgs(cfg, 160000, true)
		assert.Equal(t, []string{"proto_version '2'", "publication_names 'pub'", "messages 'true'", "streaming 'true'"}, args)
	})

	t.Run("binary opt-in on 14+", func(t *testing.T) {
		binCfg := cfg
		binCfg.Binary = true
		args := buildPluginArgs(binCfg, 160000, true)
		assert.Contains(t, args, "binary 'true'")
	})

	t.Run("single-table keeps v1 but may use binary", func(t *testing.T) {
		binCfg := cfg
		binCfg.Binary = true
		args := buildPluginArgs(binCfg, 160000, false)
		assert.Equal(t, []string{"proto_version '1'", "publication_names 'pub'", "messages 'true'", "binary 'true'"}, args)
	})

	t.Run("binary ignored on pre-14 server", func(t *testing.T) {
		binCfg := cfg
		binCfg.Binary = true
		args := buildPluginArgs(binCfg, 130000, false)
		assert.NotContains(t, args, "binary 'true'")
	})
}

func TestMultiTableReplicatorUsesProtocolV2OnlyForStreamingRuns(t *testing.T) {
	src := &PostgresCDCSource{serverVersion: 160000, lag: &lagState{}}
	cfg := CDCConfig{Publication: "pub"}

	batch, err := NewMultiTableReplicator(src, nil, cfg, 0, nil, false, "barrier")
	require.NoError(t, err)
	assert.False(t, batch.protocolV2)
	assert.NotContains(t, buildPluginArgs(cfg, src.serverVersion, batch.streaming), "streaming 'true'")

	stream, err := NewMultiTableReplicator(src, nil, cfg, 0, nil, true, "")
	require.NoError(t, err)
	assert.True(t, stream.protocolV2)
	assert.Contains(t, buildPluginArgs(cfg, src.serverVersion, stream.streaming), "streaming 'true'")
}
