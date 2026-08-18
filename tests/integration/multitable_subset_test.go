//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiTableSubsetSource writes three JSONL tables and returns a source URI
// covering all of them.
func multiTableSubsetSource(t *testing.T) string {
	t.Helper()

	usersFile := createMultiTableTestFile(t, "users", []map[string]interface{}{
		{"id": 1, "name": "alice"},
		{"id": 2, "name": "bob"},
	})
	t.Cleanup(func() { _ = os.Remove(usersFile) })

	ordersFile := createMultiTableTestFile(t, "orders", []map[string]interface{}{
		{"id": 1, "user_id": 1, "amount": 100.50},
	})
	t.Cleanup(func() { _ = os.Remove(ordersFile) })

	productsFile := createMultiTableTestFile(t, "products", []map[string]interface{}{
		{"id": 1, "name": "Widget"},
		{"id": 2, "name": "Gadget"},
		{"id": 3, "name": "Gizmo"},
	})
	t.Cleanup(func() { _ = os.Remove(productsFile) })

	return fmt.Sprintf("multitable-test://users=%s,orders=%s,products=%s", usersFile, ordersFile, productsFile)
}

// TestMultiTable_SubsetIngestsOnlySelectedTables is the pipeline-level contract:
// a subset must narrow every stage, not just the read. The excluded table's
// destination table must never be created, since destination naming, schema
// evolution and CDC state registration all iterate the discovered table set.
func TestMultiTable_SubsetIngestsOnlySelectedTables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := multiTableSubsetSource(t)

	for _, tc := range multiTableDestinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destPrefix, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTables:        []string{"users", "products"},
				DestURI:             destURI,
				DestTable:           destPrefix,
				IncrementalStrategy: config.StrategyReplace,
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			db, err := tc.sqlBackend.openDB(destURI)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			var users, products int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(fmt.Sprintf("%s.users", destPrefix))).Scan(&users))
			assert.Equal(t, 2, users)
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(fmt.Sprintf("%s.products", destPrefix))).Scan(&products))
			assert.Equal(t, 3, products)

			var orders int
			err = db.QueryRow(tc.sqlBackend.countQuery(fmt.Sprintf("%s.orders", destPrefix))).Scan(&orders)
			assert.Error(t, err, "the unselected table must not be created at the destination")
		})
	}
}

// TestMultiTable_SubsetRejectsUnknownTable guards the hard-error contract:
// silently ingesting nothing would hide a typo, and under --stream the run
// would poll forever.
func TestMultiTable_SubsetRejectsUnknownTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := multiTableSubsetSource(t)

	tc := multiTableDestinationCases()[len(multiTableDestinationCases())-1]
	destURI, destPrefix, cleanup := tc.setup(t, ctx)
	defer cleanup()

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTables:        []string{"users", "typo"},
		DestURI:             destURI,
		DestTable:           destPrefix,
		IncrementalStrategy: config.StrategyReplace,
	}
	err := pipeline.New(cfg).Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo")
}
