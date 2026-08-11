package iceberg

import (
	"context"
	"errors"
	"testing"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/stretchr/testify/require"
)

// racingCatalog reports a namespace as missing, then fails creation the way the
// sql catalog does when another writer won: a driver error, not the sentinel.
type racingCatalog struct {
	icebergcatalog.Catalog
	created     bool
	createCalls int
	createErr   error
}

func (c *racingCatalog) CheckNamespaceExists(ctx context.Context, ns icebergtable.Identifier) (bool, error) {
	return c.created, nil
}

func (c *racingCatalog) CreateNamespace(ctx context.Context, ns icebergtable.Identifier, props iceberggo.Properties) error {
	c.createCalls++
	c.created = true // the winner's row is already in place

	return c.createErr
}

func TestEnsureNamespaceToleratesConcurrentCreation(t *testing.T) {
	t.Parallel()

	driverErr := errors.New("UncheckedSQLException: Failed to execute: INSERT INTO iceberg_namespace_properties (catalog_name, namespace, property_key, property_value) VALUES (?,?,?,?)")
	cat := &racingCatalog{createErr: driverErr}
	d := &Destination{catalog: cat, cfg: icebergConfig{CreateNamespace: true}}

	require.NoError(t, d.ensureNamespace(context.Background(), icebergtable.Identifier{"raw"}))
	require.Equal(t, 1, cat.createCalls)
}

func TestEnsureNamespaceStillReportsRealFailures(t *testing.T) {
	t.Parallel()

	// Creation fails and the namespace genuinely is not there.
	cat := &neverCreatedCatalog{createErr: errors.New("permission denied")}
	d := &Destination{catalog: cat, cfg: icebergConfig{CreateNamespace: true}}

	err := d.ensureNamespace(context.Background(), icebergtable.Identifier{"raw"})
	require.ErrorContains(t, err, "failed to create namespace raw")
	require.ErrorContains(t, err, "permission denied")
}

type neverCreatedCatalog struct {
	icebergcatalog.Catalog
	createErr error
}

func (c *neverCreatedCatalog) CheckNamespaceExists(ctx context.Context, ns icebergtable.Identifier) (bool, error) {
	return false, nil
}

func (c *neverCreatedCatalog) CreateNamespace(ctx context.Context, ns icebergtable.Identifier, props iceberggo.Properties) error {
	return c.createErr
}
