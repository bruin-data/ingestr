package iceberg

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	"github.com/stretchr/testify/require"
)

func TestSQLCatalogDatabaseClosedWithDestination(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, sqliteCatalogTestURI(t, "first")))

	owned, ok := dest.catalog.(*ownedSQLCatalog)
	require.True(t, ok)
	require.NoError(t, owned.db.PingContext(ctx))
	require.NoError(t, dest.Close(ctx))
	require.Error(t, owned.db.PingContext(ctx))
	require.NoError(t, dest.Close(ctx))
}

func TestReconnectClosesPreviousSQLCatalogDatabase(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, sqliteCatalogTestURI(t, "first")))
	first := dest.catalog.(*ownedSQLCatalog)

	require.NoError(t, dest.Connect(ctx, sqliteCatalogTestURI(t, "second")))
	second := dest.catalog.(*ownedSQLCatalog)
	require.NotSame(t, first, second)
	require.Error(t, first.db.PingContext(ctx))
	require.NoError(t, second.db.PingContext(ctx))
	require.NoError(t, dest.Close(ctx))
}

func TestFailedReconnectKeepsExistingSQLCatalogOpen(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, sqliteCatalogTestURI(t, "first")))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })
	first := dest.catalog.(*ownedSQLCatalog)

	err := dest.Connect(ctx, "iceberg+sql://?sql.driver=missing-ingestr-test-driver&sql.dialect=sqlite")
	require.Error(t, err)
	require.Same(t, first, dest.catalog)
	require.NoError(t, first.db.PingContext(ctx))
}

func TestOwnedSQLCatalogPreservesPurgeCapability(t *testing.T) {
	cfg, err := parseIcebergConfig(sqliteCatalogTestURI(t, "purge"))
	require.NoError(t, err)
	cat, err := loadIcebergCatalog(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closeIcebergCatalog(cat)) })
	_, ok := cat.(icebergcatalog.PurgeableTable)
	require.True(t, ok)
}

func TestSQLCatalogNameIsolationAndLegacyDefault(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	baseURI := "iceberg+sqlite://" + filepath.Join(root, "catalog.db") +
		"?warehouse_path=" + url.QueryEscape(filepath.Join(root, "warehouse"))

	connect := func(catalogName string) *Destination {
		t.Helper()
		dest := NewDestination()
		uri := baseURI
		if catalogName != "" {
			uri += "&catalog_name=" + url.QueryEscape(catalogName)
		}
		require.NoError(t, dest.Connect(ctx, uri))
		t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })
		return dest
	}

	alpha := connect("alpha")
	beta := connect("beta")
	legacyDefault := connect("")
	explicitLegacy := connect("sql")

	require.NoError(t, alpha.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("alpha_only"), iceberggo.Properties{}))
	require.NoError(t, beta.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("beta_only"), iceberggo.Properties{}))
	require.NoError(t, legacyDefault.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("legacy_default"), iceberggo.Properties{}))

	require.Equal(t, []string{"alpha_only"}, topLevelNamespaceNames(t, ctx, alpha.catalog))
	require.Equal(t, []string{"beta_only"}, topLevelNamespaceNames(t, ctx, beta.catalog))
	require.Equal(t, []string{"legacy_default"}, topLevelNamespaceNames(t, ctx, legacyDefault.catalog))
	require.Equal(t, []string{"legacy_default"}, topLevelNamespaceNames(t, ctx, explicitLegacy.catalog))
}

func topLevelNamespaceNames(t *testing.T, ctx context.Context, cat icebergcatalog.Catalog) []string {
	t.Helper()
	namespaces, err := cat.ListNamespaces(ctx, nil)
	require.NoError(t, err)
	names := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		require.Len(t, namespace, 1)
		names = append(names, namespace[0])
	}
	return names
}

func sqliteCatalogTestURI(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	return "iceberg+sqlite://" + filepath.Join(root, name+".db") +
		"?warehouse_path=" + url.QueryEscape(filepath.Join(root, "warehouse"))
}
