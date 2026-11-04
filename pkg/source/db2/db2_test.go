package db2

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestParseDb2URI(t *testing.T) {
	cfg, err := parseDb2URI("db2://db2inst1:password@example.com/sample?schema=analytics&ssl=true&timeout=7")
	require.NoError(t, err)
	require.Equal(t, "example.com:50000", cfg.Address)
	require.Equal(t, "example.com", cfg.Host)
	require.Equal(t, "sample", cfg.Database)
	require.Equal(t, "db2inst1", cfg.User)
	require.Equal(t, "password", cfg.Password)
	require.Equal(t, "analytics", cfg.Schema)
	require.True(t, cfg.SSL)
	require.Equal(t, 7*time.Second, cfg.Timeout)
	require.Equal(t, db2PlatformAuto, cfg.Platform)
}

func TestParseDb2URIPlatformOverride(t *testing.T) {
	cfg, err := parseDb2URI("db2://db2inst1:password@example.com/siglpar?platform=ibmi")
	require.NoError(t, err)
	require.Equal(t, db2PlatformIBMi, cfg.Platform)
}

func TestParseDb2URIRDBNameOverride(t *testing.T) {
	cfg, err := parseDb2URI("db2://db2inst1:password@example.com/%2ALOCAL?platform=ibmi&rdb_name=RDBNAME2")
	require.NoError(t, err)
	require.Equal(t, "RDBNAME2", cfg.RDBName)
}

func TestParseDb2URIRejectsInvalidPlatform(t *testing.T) {
	_, err := parseDb2URI("db2://db2inst1:password@example.com/sample?platform=bogus")
	require.ErrorContains(t, err, "unsupported platform")
}

func TestParseDb2URISignonOverride(t *testing.T) {
	cfg, err := parseDb2URI("db2://db2inst1:password@example.com/siglpar?platform=ibmi&signon_host=signon.internal&signon_port=9999")
	require.NoError(t, err)
	require.Equal(t, "signon.internal", cfg.SignonHost)
	require.Equal(t, 9999, cfg.SignonPort)
}

func TestParseDb2URIRejectsInvalidSignonPort(t *testing.T) {
	_, err := parseDb2URI("db2://db2inst1:password@example.com/sample?signon_port=notanumber")
	require.ErrorContains(t, err, "invalid signon_port")
}

func TestBuildSelectQueryForSchemaWithFilters(t *testing.T) {
	start := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	end := time.Date(2024, 1, 3, 3, 4, 5, 1000, time.UTC)

	query := buildSelectQueryForSchema("orders", &schema.TableSchema{
		Schema: "ANALYTICS",
		Name:   "ORDERS",
	}, []schema.Column{
		{Name: "ID"},
		{Name: `AMOUNT"USD`},
	}, source.ReadOptions{
		IncrementalKey: "UPDATED_AT",
		IntervalStart:  &start,
		IntervalEnd:    &end,
		Limit:          10,
	})

	require.Equal(t, `SELECT "ID", "AMOUNT""USD" FROM "ANALYTICS"."ORDERS" WHERE "UPDATED_AT" >= '2024-01-02 03:04:05.123456' AND "UPDATED_AT" <= '2024-01-03 03:04:05.000001' FETCH FIRST 10 ROWS ONLY`, query)
}

func TestBuildSelectQueryForSchema(t *testing.T) {
	query := buildSelectQueryForSchema("orders", &schema.TableSchema{
		Schema: "ANALYTICS",
		Name:   "ORDERS",
	}, []schema.Column{
		{Name: "ID"},
	}, source.ReadOptions{})

	require.Equal(t, `SELECT "ID" FROM "ANALYTICS"."ORDERS"`, query)
}

func TestBuildSelectQueryForSchemaPreservesQuotedCase(t *testing.T) {
	query := buildSelectQueryForSchema(`"Mixed"."CaseTable"`, &schema.TableSchema{
		Schema: "Mixed",
		Name:   "CaseTable",
	}, []schema.Column{
		{Name: "CamelID"},
	}, source.ReadOptions{})

	require.Equal(t, `SELECT "CamelID" FROM "Mixed"."CaseTable"`, query)
}

func TestParseTableName(t *testing.T) {
	src := &Db2Source{defaultSchema: "DB2INST1"}

	schemaName, tableName := src.parseTableName("orders")
	require.Equal(t, "DB2INST1", schemaName)
	require.Equal(t, "ORDERS", tableName)

	schemaName, tableName = src.parseTableName(`"Mixed"."CaseTable"`)
	require.Equal(t, "Mixed", schemaName)
	require.Equal(t, "CaseTable", tableName)
}

func TestQuoteLiteral(t *testing.T) {
	require.Equal(t, "'O''Brien'", quoteLiteral("O'Brien"))
}
