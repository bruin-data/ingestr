package athena

import (
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestMapAthenaToDataType_Primitives(t *testing.T) {
	dt, p, s, at := MapAthenaToDataType("bigint")
	require.Equal(t, schema.TypeInt64, dt)
	require.Equal(t, 0, p)
	require.Equal(t, 0, s)
	require.Equal(t, schema.TypeUnknown, at)

	dt, _, _, _ = MapAthenaToDataType("timestamp with time zone")
	require.Equal(t, schema.TypeTimestampTZ, dt)
}

func TestMapAthenaToDataType_Decimal(t *testing.T) {
	dt, p, s, at := MapAthenaToDataType("decimal(12, 5)")
	require.Equal(t, schema.TypeDecimal, dt)
	require.Equal(t, 12, p)
	require.Equal(t, 5, s)
	require.Equal(t, schema.TypeUnknown, at)
}

func TestMapAthenaToDataType_Array(t *testing.T) {
	dt, p, s, at := MapAthenaToDataType("array<varchar>")
	require.Equal(t, schema.TypeArray, dt)
	require.Equal(t, 0, p)
	require.Equal(t, 0, s)
	require.Equal(t, schema.TypeString, at)
}
