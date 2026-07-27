package destination

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCDCLatestOverallOrderBy(t *testing.T) {
	ansi := func(s string) string { return `"` + s + `"` }
	backtick := func(s string) string { return "`" + s + "`" }
	bracket := func(s string) string { return "[" + s + "]" }

	assert.Equal(t, `"_cdc_lsn" DESC, "_cdc_deleted" DESC`, CDCLatestOverallOrderBy(ansi))
	assert.Equal(t, "`_cdc_lsn` DESC, `_cdc_deleted` DESC", CDCLatestOverallOrderBy(backtick))
	assert.Equal(t, "[_cdc_lsn] DESC, [_cdc_deleted] DESC", CDCLatestOverallOrderBy(bracket))
}

func TestCDCTargetKeyIsInjectiveForDottedComponents(t *testing.T) {
	left := CDCTargetKey("a.b", "c")
	right := CDCTargetKey("a", "b.c")
	assert.NotEqual(t, left, right)
	assert.Equal(t, left, CDCTargetKey("a.b", "c"))
}

func TestCDCTargetOwnerIDIncludesLogicalSource(t *testing.T) {
	assert.Equal(t, CDCTargetOwnerID("connector", "public.orders"), CDCTargetOwnerID("connector", "public.orders"))
	assert.NotEqual(t, CDCTargetOwnerID("connector", "public.orders"), CDCTargetOwnerID("connector", "public.customers"))
	_, err := (CDCTargetClaim{ConnectorID: "connector"}).OwnerID()
	assert.Error(t, err)
}

func TestCDCSupersedes(t *testing.T) {
	tests := []struct {
		name        string
		lsn         string
		deleted     bool
		prevLSN     string
		prevDeleted bool
		want        bool
	}{
		{"higher LSN wins", "0002", false, "0001", true, true},
		{"lower LSN loses", "0001", true, "0002", false, false},
		{"tie: delete supersedes non-delete", "0001", true, "0001", false, true},
		{"tie: non-delete does not supersede delete", "0001", false, "0001", true, false},
		{"tie: equal deleted flags keep first", "0001", true, "0001", true, false},
		{"tie: equal non-deleted keep first", "0001", false, "0001", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CDCSupersedes(tt.lsn, tt.deleted, tt.prevLSN, tt.prevDeleted))
		})
	}
}
