package tablespec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the generic Parse contract: string→typed coercion, list and
// bare-flag conventions, dotted-key nesting, and unknown-key rejection.

type parseInner struct {
	Deep string `mapstructure:"deep"`
}

type parseParams struct {
	Name  string     `mapstructure:"name"`
	Count int        `mapstructure:"count"`
	Flag  bool       `mapstructure:"flag"`
	IDs   []string   `mapstructure:"ids"`
	Nest  parseInner `mapstructure:"nest"`
}

func parseSpec(t *testing.T, raw string, opts ...ParseOption) (string, parseParams, error) {
	t.Helper()
	var out parseParams
	path, hasParams, err := Parse(raw, &out, opts...)
	if err == nil {
		require.True(t, hasParams, "expected a query block in %q", raw)
	}
	return path, out, err
}

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("scalars, comma list, bare flag, dotted nesting", func(t *testing.T) {
		t.Parallel()
		path, got, err := parseSpec(t, "tbl?name=abc&count=3&flag&ids=1,2,3&nest.deep=xyz")
		require.NoError(t, err)
		assert.Equal(t, "tbl", path)
		assert.Equal(t, "abc", got.Name)
		assert.Equal(t, 3, got.Count)
		assert.True(t, got.Flag)
		assert.Equal(t, []string{"1", "2", "3"}, got.IDs)
		assert.Equal(t, "xyz", got.Nest.Deep)
	})

	t.Run("repeated key forms a list", func(t *testing.T) {
		t.Parallel()
		_, got, err := parseSpec(t, "tbl?ids=1&ids=2")
		require.NoError(t, err)
		assert.Equal(t, []string{"1", "2"}, got.IDs)
	})

	t.Run("custom separator splits a single value", func(t *testing.T) {
		t.Parallel()
		_, got, err := parseSpec(t, "tbl?ids=a|b|c", WithListSeparator("|"))
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, got.IDs)
	})

	t.Run("explicit bool false", func(t *testing.T) {
		t.Parallel()
		_, got, err := parseSpec(t, "tbl?flag=false")
		require.NoError(t, err)
		assert.False(t, got.Flag)
	})

	t.Run("invalid bool errors", func(t *testing.T) {
		t.Parallel()
		_, _, err := parseSpec(t, "tbl?flag=maybe")
		require.Error(t, err)
	})

	t.Run("invalid int errors", func(t *testing.T) {
		t.Parallel()
		_, _, err := parseSpec(t, "tbl?count=abc")
		require.Error(t, err)
	})

	t.Run("unknown top-level key errors", func(t *testing.T) {
		t.Parallel()
		_, _, err := parseSpec(t, "tbl?bogus=1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bogus")
	})

	t.Run("unknown nested key errors", func(t *testing.T) {
		t.Parallel()
		_, _, err := parseSpec(t, "tbl?nest.bogus=1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nest.bogus")
	})

	t.Run("no query block returns path and false", func(t *testing.T) {
		t.Parallel()
		var out parseParams
		path, hasParams, err := Parse("just/a/path", &out)
		require.NoError(t, err)
		assert.False(t, hasParams)
		assert.Equal(t, "just/a/path", path)
	})
}
