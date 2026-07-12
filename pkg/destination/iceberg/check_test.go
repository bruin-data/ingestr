package iceberg

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/stretchr/testify/require"
)

func TestParseIcebergConfigValidatesCheckNamespace(t *testing.T) {
	cfg, err := parseIcebergConfig("iceberg+hadoop:///tmp/warehouse?check_namespace=lake.analytics")
	require.NoError(t, err)
	require.Equal(t, "lake.analytics", cfg.CheckNamespace)
	require.NotContains(t, cfg.Properties, "check_namespace")

	for _, namespace := range []string{"", "lake..analytics", "lake. analytics"} {
		t.Run(namespace, func(t *testing.T) {
			_, err := parseIcebergConfig("iceberg+hadoop:///tmp/warehouse?check_namespace=" + url.QueryEscape(namespace))
			require.ErrorContains(t, err, "invalid check_namespace")
		})
	}
}

func TestConnectionCheckRejectsNestedHiveNamespaceBeforeCatalogAccess(t *testing.T) {
	dest := newHadoopDestination(t)
	dest.cfg.Properties["type"] = "hive"
	dest.cfg.CreateNamespace = false
	dest.cfg.CheckNamespace = "company.analytics"

	_, _, err := dest.connectionCheckNamespace(context.Background(), "unused")
	require.ErrorContains(t, err, "Hive catalog requires a single-level namespace")
	namespaces, listErr := dest.catalog.ListNamespaces(context.Background(), nil)
	require.NoError(t, listErr)
	require.Empty(t, namespaces)
}

func TestCheckConnectionUsesConfiguredNamespace(t *testing.T) {
	ctx := context.Background()
	dest := newCheckDestination(t, "create_namespace=false&check_namespace=selected")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("aaa_unrelated"), iceberggo.Properties{}))
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("selected"), iceberggo.Properties{}))

	base := dest.catalog
	purger, ok := base.(icebergcatalog.PurgeableTable)
	require.True(t, ok)
	recording := &recordingCheckCatalog{Catalog: base, purger: purger}
	dest.catalog = recording

	require.NoError(t, dest.CheckConnection(ctx))
	created := recording.createdIdentifiers()
	require.Len(t, created, 1)
	require.Len(t, created[0], 2)
	require.Equal(t, "selected", created[0][0])
	require.Contains(t, created[0][1], "write_read_")

	for _, namespace := range []string{"aaa_unrelated", "selected"} {
		exists, err := dest.catalog.CheckNamespaceExists(ctx, icebergcatalog.ToIdentifier(namespace))
		require.NoError(t, err)
		require.True(t, exists)
	}
	requirePreparedTablesEmpty(t, dest)
}

func TestCheckConnectionRequiresConfiguredNamespaceWhenCreationDisabled(t *testing.T) {
	ctx := context.Background()
	dest := newCheckDestination(t, "create_namespace=false")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("unrelated"), iceberggo.Properties{}))

	err := dest.CheckConnection(ctx)
	require.ErrorContains(t, err, "requires check_namespace when create_namespace=false")
	requirePreparedTablesEmpty(t, dest)
}

func TestCheckConnectionRejectsMissingConfiguredNamespace(t *testing.T) {
	dest := newCheckDestination(t, "create_namespace=false&check_namespace=missing")
	err := dest.CheckConnection(context.Background())
	require.ErrorContains(t, err, `configured check_namespace "missing" does not exist`)
	requirePreparedTablesEmpty(t, dest)
}

func TestCheckConnectionIgnoresConfiguredPartitionSpec(t *testing.T) {
	dest := newCheckDestination(t, "partition_spec=missing_check_column")
	require.Equal(t, "missing_check_column", dest.cfg.PartitionSpec)
	base := dest.catalog
	purger, ok := base.(icebergcatalog.PurgeableTable)
	require.True(t, ok)
	recording := &recordingCheckCatalog{Catalog: base, purger: purger}
	dest.catalog = recording

	require.NoError(t, dest.CheckConnection(context.Background()))
	require.Equal(t, []int{0}, recording.createdPartitionFieldCounts())
	requirePreparedTablesEmpty(t, dest)
}

func TestCheckConnectionPreservesUnrelatedConfiguredLocationContents(t *testing.T) {
	warehouse := t.TempDir()
	shared := filepath.Join(warehouse, "shared-table-location")
	sentinelDir := filepath.Join(shared, "unrelated-empty-directory")
	require.NoError(t, os.MkdirAll(sentinelDir, 0o755))
	sentinelFile := filepath.Join(shared, "unrelated-sentinel.txt")
	require.NoError(t, os.WriteFile(sentinelFile, []byte("keep me"), 0o600))

	catalogPath := filepath.Join(t.TempDir(), "catalog.db")
	rawURI := "iceberg+sqlite://" + catalogPath +
		"?warehouse_path=" + url.QueryEscape(warehouse) +
		"&table_location=" + url.QueryEscape(filepath.Join(shared, "{identifier}"))
	dest := NewDestination()
	require.NoError(t, dest.Connect(context.Background(), rawURI))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	require.NoError(t, dest.CheckConnection(context.Background()))

	contents, err := os.ReadFile(sentinelFile)
	require.NoError(t, err)
	require.Equal(t, "keep me", string(contents))
	info, err := os.Stat(sentinelDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	requirePreparedTablesEmpty(t, dest)
}

func TestCheckConnectionClearsPreparedEntryAfterFailure(t *testing.T) {
	dest := newCheckDestination(t, "")
	base := dest.catalog
	purger, ok := base.(icebergcatalog.PurgeableTable)
	require.True(t, ok)
	dest.catalog = &failOnceLoadCheckCatalog{Catalog: base, purger: purger}

	err := dest.CheckConnection(context.Background())
	require.ErrorContains(t, err, "write test row")
	requirePreparedTablesEmpty(t, dest)
}

func newCheckDestination(t *testing.T, query string) *Destination {
	t.Helper()
	return newCheckDestinationWithWarehouse(t, t.TempDir(), query)
}

func newCheckDestinationWithWarehouse(t *testing.T, warehouse, query string) *Destination {
	t.Helper()
	rawURI := "iceberg+hadoop://?warehouse=" + url.QueryEscape(warehouse)
	if query != "" {
		rawURI += "&" + query
	}
	dest := NewDestination()
	require.NoError(t, dest.Connect(context.Background(), rawURI))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	return dest
}

func requirePreparedTablesEmpty(t *testing.T, dest *Destination) {
	t.Helper()
	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Empty(t, dest.prepared)
}

type recordingCheckCatalog struct {
	icebergcatalog.Catalog
	purger icebergcatalog.PurgeableTable
	mu     sync.Mutex
	idents []icebergtable.Identifier
	parts  []int
}

func (c *recordingCheckCatalog) CreateTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	tableSchema *iceberggo.Schema,
	opts ...icebergcatalog.CreateTableOpt,
) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.CreateTable(ctx, ident, tableSchema, opts...)
	if err != nil {
		return nil, err
	}
	partitionSpec := tbl.Metadata().PartitionSpec()
	if strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
		return tbl, nil
	}
	c.mu.Lock()
	c.idents = append(c.idents, append(icebergtable.Identifier(nil), ident...))
	c.parts = append(c.parts, partitionSpec.NumFields())
	c.mu.Unlock()
	return tbl, nil
}

func (c *recordingCheckCatalog) PurgeTable(ctx context.Context, ident icebergtable.Identifier) error {
	return c.purger.PurgeTable(ctx, ident)
}

func (c *recordingCheckCatalog) createdIdentifiers() []icebergtable.Identifier {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]icebergtable.Identifier, len(c.idents))
	for i, ident := range c.idents {
		result[i] = append(icebergtable.Identifier(nil), ident...)
	}
	return result
}

func (c *recordingCheckCatalog) createdPartitionFieldCounts() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.parts...)
}

type failOnceLoadCheckCatalog struct {
	icebergcatalog.Catalog
	purger icebergcatalog.PurgeableTable
	mu     sync.Mutex
	failed bool
}

func (c *failOnceLoadCheckCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	c.mu.Lock()
	if !c.failed {
		c.failed = true
		c.mu.Unlock()
		return nil, icebergcatalog.ErrNoSuchTable
	}
	c.mu.Unlock()
	return c.Catalog.LoadTable(ctx, ident)
}

func (c *failOnceLoadCheckCatalog) PurgeTable(ctx context.Context, ident icebergtable.Identifier) error {
	return c.purger.PurgeTable(ctx, ident)
}
