//go:build integration

package integration

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
	"github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresPrimaryKeyDiscoverySelectOnlyRole(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	adminURI := sharedPostgresURI(t, "source")
	admin, err := pgxpool.New(ctx, adminURI)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schemaName := uniqueSchemaName(t, "pk") + ` "Case'`
	roleName := uniqueSchemaName(t, "reader")
	publication := uniqueSchemaName(t, "publication")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, statement := range []string{
			"DROP PUBLICATION IF EXISTS " + pqIdent(publication),
			"DROP SCHEMA IF EXISTS " + pqIdent(schemaName) + " CASCADE",
			"DROP ROLE IF EXISTS " + pqIdent(roleName),
		} {
			_, err := admin.Exec(cleanupCtx, statement)
			assert.NoError(t, err)
		}
	})
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+pqIdent(schemaName))
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "CREATE ROLE "+pqIdent(roleName)+" LOGIN REPLICATION PASSWORD 'select_only_test'")
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "GRANT USAGE ON SCHEMA "+pqIdent(schemaName)+" TO "+pqIdent(roleName))
	require.NoError(t, err)

	testCases := []struct {
		name        string
		columns     string
		primaryKeys []string
		cdcKeys     []string
	}{
		{
			name:        "single_key",
			columns:     "id bigint PRIMARY KEY, payload text",
			primaryKeys: []string{"id"},
		},
		{
			name:        "composite_include",
			columns:     "id bigint, tenant_id bigint, payload text, PRIMARY KEY (tenant_id, id) INCLUDE (payload)",
			primaryKeys: []string{"tenant_id", "id"},
		},
		{
			name:        `Quoted "Table's`,
			columns:     `"Row ""ID" bigint, "Tenant'ID" bigint, payload text, PRIMARY KEY ("Tenant'ID", "Row ""ID")`,
			primaryKeys: []string{"Tenant'ID", `Row "ID`},
		},
		{
			name:    "no_primary_key",
			columns: "id bigint, payload text",
		},
		{
			name:        "replica_identity",
			columns:     "id bigint PRIMARY KEY, code text NOT NULL, payload text NOT NULL",
			primaryKeys: []string{"id"},
			cdcKeys:     []string{"code"},
		},
	}
	var publishedTables []string
	for _, tc := range testCases {
		tableName := pqTable(schemaName, tc.name)
		_, err = admin.Exec(ctx, "CREATE TABLE "+tableName+" ("+tc.columns+")")
		require.NoError(t, err)
		if tc.cdcKeys != nil {
			_, err = admin.Exec(ctx, "CREATE UNIQUE INDEX identity_code ON "+tableName+" (code) INCLUDE (payload)")
			require.NoError(t, err)
			_, err = admin.Exec(ctx, "ALTER TABLE "+tableName+" REPLICA IDENTITY USING INDEX identity_code")
			require.NoError(t, err)
		}
		if len(tc.primaryKeys) == 0 {
			_, err = admin.Exec(ctx, "ALTER TABLE "+tableName+" REPLICA IDENTITY FULL")
			require.NoError(t, err)
		}
		_, err = admin.Exec(ctx, "GRANT SELECT ON TABLE "+tableName+" TO "+pqIdent(roleName))
		require.NoError(t, err)
		publishedTables = append(publishedTables, tableName)
	}
	_, err = admin.Exec(ctx, "CREATE PUBLICATION "+pqIdent(publication)+" FOR TABLE "+strings.Join(publishedTables, ", "))
	require.NoError(t, err)

	readerURI := postgresURIWithUser(t, adminURI, roleName, "select_only_test")
	reader, err := pgxpool.New(ctx, readerURI)
	require.NoError(t, err)
	t.Cleanup(reader.Close)
	for _, tc := range testCases {
		var canSelect, canReference, canWrite bool
		err = reader.QueryRow(ctx, `SELECT has_table_privilege($1, 'SELECT'),
			has_table_privilege($1, 'REFERENCES'),
			has_table_privilege($1, 'INSERT, UPDATE, DELETE, TRUNCATE, TRIGGER')`,
			pqTable(schemaName, tc.name)).Scan(&canSelect, &canReference, &canWrite)
		require.NoError(t, err)
		require.True(t, canSelect)
		require.False(t, canReference)
		require.False(t, canWrite)
	}
	var visibleConstraints int
	err = reader.QueryRow(ctx, `SELECT count(*) FROM information_schema.table_constraints
		WHERE table_schema = $1 AND constraint_type = 'PRIMARY KEY'`, schemaName).Scan(&visibleConstraints)
	require.NoError(t, err)
	require.Zero(t, visibleConstraints, "information_schema hides primary keys from this SELECT-only role")

	cdcURI, err := url.Parse(readerURI)
	require.NoError(t, err)
	cdcURI.Scheme = "postgres+cdc"
	params := cdcURI.Query()
	params.Set("publication", publication)
	cdcURI.RawQuery = params.Encode()
	for _, connector := range []struct {
		name string
		src  source.Source
		uri  string
	}{
		{name: "postgres", src: postgres.NewPostgresSource(), uri: readerURI},
		{name: "postgres_cdc", src: postgres_cdc.NewPostgresCDCSource(), uri: cdcURI.String()},
	} {
		t.Run(connector.name, func(t *testing.T) {
			require.NoError(t, connector.src.Connect(ctx, connector.uri))
			t.Cleanup(func() { assert.NoError(t, connector.src.Close(context.Background())) })
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					expectedKeys := tc.primaryKeys
					if connector.name == "postgres_cdc" && tc.cdcKeys != nil {
						expectedKeys = tc.cdcKeys
					}
					table, err := connector.src.GetTable(ctx, source.TableRequest{Name: schemaName + "." + tc.name})
					require.NoError(t, err)
					require.True(t, slices.Equal(expectedKeys, table.PrimaryKeys()),
						"expected primary keys %v, got %v", expectedKeys, table.PrimaryKeys())
					tableSchema, err := table.GetSchema(ctx)
					require.NoError(t, err)
					require.Equal(t, schemaName, tableSchema.Schema)
					require.Equal(t, tc.name, tableSchema.Name)
					require.True(t, slices.Equal(expectedKeys, tableSchema.PrimaryKeys),
						"expected primary keys %v, got %v", expectedKeys, tableSchema.PrimaryKeys)
					for _, col := range tableSchema.Columns {
						assert.Equal(t, slices.Contains(expectedKeys, col.Name), col.IsPrimaryKey,
							"primary key flag for %q", col.Name)
					}
				})
			}
		})
	}
}
