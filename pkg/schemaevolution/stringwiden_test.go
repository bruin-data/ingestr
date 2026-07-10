package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringNeedsWidening(t *testing.T) {
	cases := []struct {
		src, dest int
		want      bool
	}{
		{50, 50, false}, // equal
		{100, 50, true}, // longer
		{30, 50, false}, // shorter -> never narrow
		{0, 50, true},   // unbounded source over bounded dest
		{50, 0, false},  // dest already unbounded
		{0, 0, false},   // both unbounded
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, stringNeedsWidening(c.src, c.dest), "src=%d dest=%d", c.src, c.dest)
	}
}

func TestWidenedStringLength(t *testing.T) {
	assert.Equal(t, 100, WidenedStringLength(100, 50))
	assert.Equal(t, 100, WidenedStringLength(50, 100))
	assert.Equal(t, 0, WidenedStringLength(0, 50)) // unbounded wins
	assert.Equal(t, 0, WidenedStringLength(50, 0))
}

func strCol(name string, maxLen int) schema.Column {
	return schema.Column{Name: name, DataType: schema.TypeString, MaxLength: maxLen, Nullable: true}
}

func TestCompare_StringWiden_AutoDetected(t *testing.T) {
	src := &schema.TableSchema{Columns: []schema.Column{strCol("name", 0)}} // source now unbounded/wider
	dest := &schema.TableSchema{Columns: []schema.Column{strCol("name", 50)}}

	cmp, err := Compare(src, dest, nil)
	require.NoError(t, err)
	require.True(t, cmp.HasChanges)
	require.Len(t, cmp.Changes, 1)
	assert.Equal(t, ChangeWidenType, cmp.Changes[0].Type)
	assert.Equal(t, 0, cmp.Changes[0].NewColumn.MaxLength) // widened to unbounded
}

func TestCompare_StringWiden_ViaOverride(t *testing.T) {
	src := &schema.TableSchema{Columns: []schema.Column{strCol("name", 0)}}
	dest := &schema.TableSchema{Columns: []schema.Column{strCol("name", 50)}}
	overrides, err := ParseColumnOverrides("name:varchar(100)")
	require.NoError(t, err)

	cmp, err := Compare(src, dest, &CompareOptions{Overrides: overrides})
	require.NoError(t, err)
	require.Len(t, cmp.Changes, 1)
	assert.Equal(t, ChangeOverrideType, cmp.Changes[0].Type)
	assert.Equal(t, 100, cmp.Changes[0].NewColumn.MaxLength)
}

func TestCompare_StringNoNarrow(t *testing.T) {
	// dest varchar(100), override asks for varchar(50): must NOT narrow.
	src := &schema.TableSchema{Columns: []schema.Column{strCol("name", 0)}}
	dest := &schema.TableSchema{Columns: []schema.Column{strCol("name", 100)}}
	overrides, err := ParseColumnOverrides("name:varchar(50)")
	require.NoError(t, err)

	cmp, err := Compare(src, dest, &CompareOptions{Overrides: overrides})
	require.NoError(t, err)
	assert.False(t, cmp.HasChanges, "should not narrow an existing string column")
}

func TestCompare_StringStable_NoSpuriousChange(t *testing.T) {
	// Same length on both sides must not produce a change (avoid ALTER churn).
	src := &schema.TableSchema{Columns: []schema.Column{strCol("name", 50)}}
	dest := &schema.TableSchema{Columns: []schema.Column{strCol("name", 50)}}
	cmp, err := Compare(src, dest, nil)
	require.NoError(t, err)
	assert.False(t, cmp.HasChanges)
}

func TestCompare_DecimalToExistingUnboundedString_NoSpuriousChange(t *testing.T) {
	src := &schema.TableSchema{Columns: []schema.Column{{
		Name:      "all_revenue_total_d90",
		DataType:  schema.TypeDecimal,
		Precision: 38,
		Scale:     9,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{strCol("all_revenue_total_d90", 0)}}

	cmp, err := Compare(src, dest, nil)
	require.NoError(t, err)
	assert.False(t, cmp.HasChanges)
	assert.Empty(t, cmp.Changes)
}
