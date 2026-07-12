package postgres_cdc

import (
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURIConfigDiscoverInterval(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    time.Duration
		wantErr bool
	}{
		{
			name: "default when absent",
			uri:  "postgres+cdc://user:pass@localhost:5432/mydb",
			want: defaultDiscoverInterval,
		},
		{
			name: "explicit duration",
			uri:  "postgres+cdc://user:pass@localhost:5432/mydb?discover_interval=5s",
			want: 5 * time.Second,
		},
		{
			name: "disabled with zero",
			uri:  "postgres+cdc://user:pass@localhost:5432/mydb?discover_interval=0",
			want: 0,
		},
		{
			name: "disabled with off",
			uri:  "postgres+cdc://user:pass@localhost:5432/mydb?discover_interval=off",
			want: 0,
		},
		{
			name:    "invalid duration",
			uri:     "postgres+cdc://user:pass@localhost:5432/mydb?discover_interval=bogus",
			wantErr: true,
		},
		{
			name:    "negative duration",
			uri:     "postgres+cdc://user:pass@localhost:5432/mydb?discover_interval=-5s",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, normalizedURI, err := parseURIConfig(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.DiscoverInterval)
			assert.NotContains(t, normalizedURI, "discover_interval=")
		})
	}
}

func TestBackfillSlotName(t *testing.T) {
	assert.Equal(t, "ingestr_mt_pub_bf", backfillSlotName("ingestr_mt_pub"))

	long := strings.Repeat("x", 70)
	got := backfillSlotName(long)
	assert.LessOrEqual(t, len(got), 63)
	assert.True(t, strings.HasSuffix(got, "_bf"))

	// A main slot name already at the 63-char limit must still produce a
	// distinct backfill name.
	atLimit := strings.Repeat("y", 63)
	assert.NotEqual(t, atLimit, backfillSlotName(atLimit))
}

func TestPublicationTableFullName(t *testing.T) {
	assert.Equal(t, "public.users", publicationTableFullName("public", "users"))
	assert.Equal(t, "app.orders", publicationTableFullName("app", "orders"))
}

func TestDiffNewTables(t *testing.T) {
	current := []source.SourceTableInfo{
		{Name: "users"},
		{Name: "app.orders"},
	}

	assert.Empty(t, diffNewTables(current, map[string]struct{}{
		"users":      {},
		"app.orders": {},
	}))

	got := diffNewTables(current, map[string]struct{}{
		"users":      {},
		"app.orders": {},
		"products":   {},
		"app.events": {},
	})
	assert.Equal(t, []string{"app.events", "products"}, got)

	// Tables that disappeared from the source are not reported.
	assert.Empty(t, diffNewTables(current, map[string]struct{}{"users": {}}))
}

func TestNewlyObservedTablesOnlyReturnsTablesAddedAfterPipelinePreparation(t *testing.T) {
	tables := []source.SourceTableInfo{
		{Name: "users", Incarnation: "10"},
		{Name: "orders", Incarnation: "20"},
		{Name: "products", Incarnation: "30"},
	}
	got := newlyObservedTables(tables, []string{"users", "orders"})
	require.Equal(t, []source.SourceTableInfo{{Name: "products", Incarnation: "30"}}, got)
	assert.Equal(t, tables, newlyObservedTables(tables, []string{}), "an empty prepared set must announce every table observed by ReadAll")
	assert.Nil(t, newlyObservedTables(tables, nil))
}

func TestFilterKnownTablesFreezesBatchStartupSet(t *testing.T) {
	tables := []source.SourceTableInfo{{Name: "users"}, {Name: "orders"}, {Name: "products"}}
	require.Equal(t, []source.SourceTableInfo{{Name: "users"}, {Name: "orders"}}, filterKnownTables(tables, []string{"orders", "users"}))
	assert.Empty(t, filterKnownTables(tables, []string{}))
}

func TestDiscoveryWaitsForCommittedTransactionSpool(t *testing.T) {
	repl := &fakeReplicator{pendingLowWater: func() (pglogrepl.LSN, bool) { return 42, true }}
	assert.False(t, discoveryReady(repl))
	repl.pendingLowWater = func() (pglogrepl.LSN, bool) { return 0, false }
	assert.True(t, discoveryReady(repl))
}

func TestDiffTableIncarnationsDetectsSameNameRecreation(t *testing.T) {
	current := []source.SourceTableInfo{
		{Name: "public.orders", Incarnation: "100"},
		{Name: "public.customers", Incarnation: "200"},
	}
	added, recreated, err := diffTableIncarnations(current, map[string]string{
		"public.orders":    "101",
		"public.customers": "200",
		"public.invoices":  "300",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"public.invoices"}, added)
	require.Equal(t, []string{"public.orders"}, recreated)
}

func TestTablesWithoutResumeState(t *testing.T) {
	tables := []source.SourceTableInfo{
		{Name: "users"},
		{Name: "orders"},
		{Name: "products"},
	}

	r := NewMultiTableCDCReader(nil, tables, CDCConfig{}, map[string]string{
		"users": "00000000/000000A0",
	}, "")

	// orders and products lack resume LSNs and processed LSNs → backfill.
	got := r.tablesWithoutResumeState()
	names := make([]string, len(got))
	for i, tbl := range got {
		names[i] = tbl.Name
	}
	assert.Equal(t, []string{"orders", "products"}, names)

	// A table with an in-memory processed LSN (e.g. just backfilled) is not
	// backfilled again.
	r.updateProcessedLSN("orders", pglogrepl.LSN(100))
	got = r.tablesWithoutResumeState()
	require.Len(t, got, 1)
	assert.Equal(t, "products", got[0].Name)
}
