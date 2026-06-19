package tablespec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the generic Decode contract: string→typed coercion, list and
// bare-flag conventions, dotted-key nesting, and unknown-key rejection.

type decodeInner struct {
	Deep string `mapstructure:"deep"`
}

type decodeParams struct {
	Name  string      `mapstructure:"name"`
	Count int         `mapstructure:"count"`
	Flag  bool        `mapstructure:"flag"`
	IDs   []string    `mapstructure:"ids"`
	Nest  decodeInner `mapstructure:"nest"`
}

type decodeCfg struct {
	Table  string       `mapstructure:"table"`
	Params decodeParams `mapstructure:"parameters"`
}

func decodeSpec(t *testing.T, raw string, opts ...DecodeOption) (decodeCfg, error) {
	t.Helper()
	path, params, hasQuery, err := Split(raw)
	require.NoError(t, err)
	require.True(t, hasQuery, "expected a query block in %q", raw)
	var out decodeCfg
	return out, Decode(path, params, &out, opts...)
}

func TestDecode(t *testing.T) {
	t.Parallel()

	t.Run("scalars, comma list, bare flag, dotted nesting", func(t *testing.T) {
		t.Parallel()
		got, err := decodeSpec(t, "tbl?name=abc&count=3&flag&ids=1,2,3&nest.deep=xyz")
		require.NoError(t, err)
		assert.Equal(t, "tbl", got.Table)
		assert.Equal(t, "abc", got.Params.Name)
		assert.Equal(t, 3, got.Params.Count)
		assert.True(t, got.Params.Flag)
		assert.Equal(t, []string{"1", "2", "3"}, got.Params.IDs)
		assert.Equal(t, "xyz", got.Params.Nest.Deep)
	})

	t.Run("repeated key forms a list", func(t *testing.T) {
		t.Parallel()
		got, err := decodeSpec(t, "tbl?ids=1&ids=2")
		require.NoError(t, err)
		assert.Equal(t, []string{"1", "2"}, got.Params.IDs)
	})

	t.Run("custom separator splits a single value", func(t *testing.T) {
		t.Parallel()
		got, err := decodeSpec(t, "tbl?ids=a|b|c", WithListSeparator("|"))
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, got.Params.IDs)
	})

	t.Run("explicit bool false", func(t *testing.T) {
		t.Parallel()
		got, err := decodeSpec(t, "tbl?flag=false")
		require.NoError(t, err)
		assert.False(t, got.Params.Flag)
	})

	t.Run("invalid bool errors", func(t *testing.T) {
		t.Parallel()
		_, err := decodeSpec(t, "tbl?flag=maybe")
		require.Error(t, err)
	})

	t.Run("invalid int errors", func(t *testing.T) {
		t.Parallel()
		_, err := decodeSpec(t, "tbl?count=abc")
		require.Error(t, err)
	})

	t.Run("unknown top-level key errors", func(t *testing.T) {
		t.Parallel()
		_, err := decodeSpec(t, "tbl?bogus=1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bogus")
	})

	t.Run("unknown nested key errors", func(t *testing.T) {
		t.Parallel()
		_, err := decodeSpec(t, "tbl?nest.bogus=1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nest.bogus")
	})
}
