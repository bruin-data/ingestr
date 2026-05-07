package integration

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	schemaContractInitialRows    = 5
	schemaContractNewColumnRows  = 3
	schemaContractTypeChangeRows = 3
)

// TestDestinations_SchemaContract_Freeze validates that freeze mode rejects schema changes.
// - First load: create table with id, name, age(int), score
// - Second load: try to add a new column (email) with freeze mode
// - Expected: pipeline should fail with contract violation error
func TestDestinations_SchemaContract_Freeze(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	newColumnURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_freeze_new_column", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_freeze_new_column", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load: create table with initial schema
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "evolve", // Default - allow creation
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Validate initial load
			validateContractInitialSQL(t, tc.sqlBackend, destURI, destTable)

			// Second load: with freeze mode, new column should fail
			cfg.SourceURI = newColumnURI
			cfg.SourceTable = "contract_new_column"
			cfg.SchemaContract = "freeze"

			p2 := pipeline.New(cfg)
			err := p2.Run(ctx)
			require.Error(t, err, "Freeze mode should reject new column")
			assert.Contains(t, err.Error(), "schema contract violation", "Error should mention contract violation")

			// Verify table still has only initial rows (no partial writes)
			validateContractRowCountSQL(t, tc.sqlBackend, destURI, destTable, schemaContractInitialRows)
		})
	}
}

// TestDestinations_SchemaContract_Freeze_TypeChange validates freeze mode rejects type changes.
func TestDestinations_SchemaContract_Freeze_TypeChange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	typeChangeURI := jsonlURI(t, "testdata/conformance_contract_type_change.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_freeze_type_change", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_freeze_type_change", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load: create table with initial schema (age is int)
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Second load: with freeze mode, type change (age int->string) should fail
			cfg.SourceURI = typeChangeURI
			cfg.SourceTable = "contract_type_change"
			cfg.SchemaContract = "freeze"

			p2 := pipeline.New(cfg)
			err := p2.Run(ctx)
			require.Error(t, err, "Freeze mode should reject type change")
			assert.Contains(t, err.Error(), "schema contract violation", "Error should mention contract violation")
		})
	}
}

// TestDestinations_SchemaContract_Evolve validates that evolve mode (default) allows schema changes.
func TestDestinations_SchemaContract_Evolve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	newColumnURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_evolve", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_evolve", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Second load: with evolve mode, new column should succeed
			cfg.SourceURI = newColumnURI
			cfg.SourceTable = "contract_new_column"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Evolve mode should allow new column")

			// Verify all rows are present
			validateContractRowCountSQL(t, tc.sqlBackend, destURI, destTable, schemaContractInitialRows+schemaContractNewColumnRows)

			// Verify email column was added
			validateContractEmailColumnExists(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// TestDestinations_SchemaContract_DiscardValue validates discard_value mode:
// - Allows new columns to be added
// - Type widening changes are not applied (but pipeline succeeds with warning)
func TestDestinations_SchemaContract_DiscardValue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	newColumnURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_discard_value", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_discard_value", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Second load: discard_value mode should allow new columns
			cfg.SourceURI = newColumnURI
			cfg.SourceTable = "contract_new_column"
			cfg.SchemaContract = "discard_value"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Discard value mode should succeed with new column")

			// Verify new column was added (discard_value allows ChangeAddColumn)
			validateContractEmailColumnExists(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// TestDestinations_SchemaContract_DiscardValue_TypeChange validates that
// discard_value mode allows ingestion by NULLing out incompatible values.
// The destination schema should NOT be widened, but new rows are added with NULLs for incompatible columns.
func TestDestinations_SchemaContract_DiscardValue_TypeChange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	typeChangeURI := jsonlURI(t, "testdata/conformance_contract_type_change.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_discard_value_type_change", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_discard_value_type_change", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Get initial age column type
			initialTypes := getColumnTypes(t, tc.sqlBackend, destURI, destTable)
			initialAgeType := initialTypes["age"]

			// Second load: discard_value mode with type change
			// discard_value NULLs out values that don't match the destination schema type
			cfg.SourceURI = typeChangeURI
			cfg.SourceTable = "contract_type_change"
			cfg.SchemaContract = "discard_value"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Discard value mode should succeed and ingest with NULLs")

			// Verify age column type was NOT widened (schema evolution skips type changes)
			finalTypes := getColumnTypes(t, tc.sqlBackend, destURI, destTable)
			assert.Equal(t, initialAgeType, finalTypes["age"], "Age column type should NOT change with discard_value")

			// Verify new rows were written (with age values NULLed out due to type mismatch)
			validateContractRowCountSQL(t, tc.sqlBackend, destURI, destTable, schemaContractInitialRows+schemaContractTypeChangeRows)
		})
	}
}

// TestDestinations_SchemaContract_DiscardRow validates discard_row mode:
// - Rows with incompatible values (e.g., new columns with values) are filtered out
// - Schema evolution still applies new columns, but rows with those values are discarded
// - Pipeline succeeds and older rows are preserved
func TestDestinations_SchemaContract_DiscardRow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	newColumnURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_discard_row", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_discard_row", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Get initial column count
			initialTypes := getColumnTypes(t, tc.sqlBackend, destURI, destTable)
			initialColCount := len(initialTypes)

			// Second load: discard_row mode - filters out rows with new columns
			// The email column is added to the schema, but rows that have values in it are discarded
			cfg.SourceURI = newColumnURI
			cfg.SourceTable = "contract_new_column"
			cfg.SchemaContract = "discard_row"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Discard row mode should succeed and filter incompatible rows")

			// Verify new columns WERE added (schema evolution applies allowed changes)
			finalTypes := getColumnTypes(t, tc.sqlBackend, destURI, destTable)
			assert.Greater(t, len(finalTypes), initialColCount, "New columns should be added even in discard_row mode")

			// Verify only initial rows were preserved (new rows with email values were discarded)
			validateContractRowCountSQL(t, tc.sqlBackend, destURI, destTable, schemaContractInitialRows)

			// Verify email column exists in schema
			validateContractEmailColumnExists(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// TestDestinations_SchemaContract_DefaultIsEvolve validates that empty/default contract is evolve.
func TestDestinations_SchemaContract_DefaultIsEvolve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	newColumnURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_default", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_default", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load with empty contract (should default to evolve)
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "contract_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},

				SchemaContract: "", // Empty = default to evolve
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Second load with empty contract - should allow new column
			cfg.SourceURI = newColumnURI
			cfg.SourceTable = "contract_new_column"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Default (evolve) mode should allow new column")

			// Verify new column was added
			validateContractEmailColumnExists(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// Helper functions

func validateContractInitialSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, schemaContractInitialRows, count, "Should have initial rows")
}

func validateContractRowCountSQL(t *testing.T, backend *sqlBackend, uri, table string, expected int) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, expected, count, "Row count mismatch")
}

func validateContractEmailColumnExists(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	types := getColumnTypes(t, backend, uri, table)
	_, exists := types["email"]
	assert.True(t, exists, "Email column should exist after schema evolution")
}

func getColumnTypes(t *testing.T, backend *sqlBackend, uri, table string) map[string]string {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend: %v", err)
		return nil
	}
	defer func() { _ = db.Close() }()

	types, err := backend.schemaTypes(db, table)
	if err != nil {
		t.Skipf("Could not read schema types: %v", err)
		return nil
	}
	return types
}

// TestDestinations_SchemaContract_ColumnRemoval_Evolve validates that evolve mode allows column removal.
// - First load: create table with id, name, age, score, email
// - Second load: source loses email column (Fivetran-style soft removal)
// - Expected: email column stays in destination, new rows have NULL for email
func TestDestinations_SchemaContract_ColumnRemoval_Evolve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	withEmailURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")
	withoutEmailURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_column_removal_evolve", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_column_removal_evolve", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load: create table with email column
			cfg := &config.IngestConfig{
				SourceURI:           withEmailURI,
				SourceTable:         "contract_new_column",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},
				SchemaContract:      "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Verify email column exists after first load
			types := getColumnTypes(t, tc.sqlBackend, destURI, destTable)
			_, hasEmail := types["email"]
			require.True(t, hasEmail, "Email column should exist after first load")

			// Second load: source no longer has email column
			// Should succeed with warning, email column stays but new rows have NULL
			cfg.SourceURI = withoutEmailURI
			cfg.SourceTable = "contract_initial"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Evolve mode should allow column removal (soft delete)")

			// Verify email column still exists
			typesAfter := getColumnTypes(t, tc.sqlBackend, destURI, destTable)
			_, hasEmailAfter := typesAfter["email"]
			assert.True(t, hasEmailAfter, "Email column should still exist after column removal")

			// Verify all rows are present
			validateContractRowCountSQL(t, tc.sqlBackend, destURI, destTable, schemaContractNewColumnRows+schemaContractInitialRows)
		})
	}
}

// TestDestinations_SchemaContract_ColumnRemoval_Freeze validates that freeze mode rejects column removal.
func TestDestinations_SchemaContract_ColumnRemoval_Freeze(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	withEmailURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")
	withoutEmailURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name+"_column_removal_freeze", func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name+"_column_removal_freeze", func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load: create table with email column
			cfg := &config.IngestConfig{
				SourceURI:           withEmailURI,
				SourceTable:         "contract_new_column",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},
				SchemaContract:      "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Second load: source no longer has email column
			// Freeze mode should reject this as a schema change
			cfg.SourceURI = withoutEmailURI
			cfg.SourceTable = "contract_initial"
			cfg.SchemaContract = "freeze"

			p2 := pipeline.New(cfg)
			err := p2.Run(ctx)
			require.Error(t, err, "Freeze mode should reject column removal")
			assert.Contains(t, err.Error(), "schema contract violation", "Error should mention contract violation")
		})
	}
}

// TestDestinations_SchemaContract_InvalidMode validates that invalid modes are rejected.
func TestDestinations_SchemaContract_InvalidMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_contract_initial.jsonl")
	newColumnURI := jsonlURI(t, "testdata/conformance_contract_new_column.jsonl")

	// Test with just one destination (postgres or duckdb)
	var tc *destCase
	for _, c := range destinationCases() {
		if c.sqlBackend != nil && c.schemaEvolutionCapable {
			cCopy := c
			tc = &cCopy
			break
		}
	}

	if tc == nil {
		t.Skip("No suitable destination for invalid mode test")
	}

	t.Run(tc.name+"_invalid_mode", func(t *testing.T) {
		destURI, destTable, cleanup := tc.setup(t, ctx)
		defer cleanup()

		// First load
		cfg := &config.IngestConfig{
			SourceURI:           initialURI,
			SourceTable:         "contract_initial",
			DestURI:             destURI,
			DestTable:           destTable,
			IncrementalStrategy: config.StrategyMerge,
			PrimaryKeys:         []string{"id"},

			SchemaContract: "evolve",
		}

		p1 := pipeline.New(cfg)
		require.NoError(t, p1.Run(ctx), "First load should succeed")

		// Second load with invalid mode
		cfg.SourceURI = newColumnURI
		cfg.SourceTable = "contract_new_column"
		cfg.SchemaContract = "invalid_mode"

		p2 := pipeline.New(cfg)
		err := p2.Run(ctx)
		require.Error(t, err, "Invalid mode should be rejected")
		assert.Contains(t, err.Error(), "unknown schema contract mode", "Error should mention unknown mode")
	})
}
