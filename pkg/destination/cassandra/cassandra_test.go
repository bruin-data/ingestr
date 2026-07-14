package cassandra

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestBuildCreateKeyspaceSQLIsRaceSafe(t *testing.T) {
	require.Equal(
		t,
		`CREATE KEYSPACE IF NOT EXISTS "_bruin_staging" WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3}`,
		buildCreateKeyspaceSQL("_bruin_staging", 3),
	)
}

func TestBuildCreateTableSQL(t *testing.T) {
	sch := []schema.Column{
		{Name: "tenant_id", DataType: schema.TypeString},
		{Name: "id", DataType: schema.TypeUUID},
		{Name: "amount", DataType: schema.TypeDecimal},
		{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString},
	}

	sql, err := buildCreateTableSQL(`"analytics"."events"`, sch, []string{"tenant_id", "id"})
	require.NoError(t, err)
	require.Equal(t, `CREATE TABLE IF NOT EXISTS "analytics"."events" ("tenant_id" text, "id" uuid, "amount" decimal, "tags" list<text>, PRIMARY KEY (("tenant_id"), "id"))`, sql)
}

func TestBuildCreateTableSQLRequiresPrimaryKey(t *testing.T) {
	_, err := buildCreateTableSQL(`"ks"."events"`, []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, nil)
	require.ErrorContains(t, err, "requires at least one primary key")
}

func TestDialectAddColumnSQL(t *testing.T) {
	d := &Dialect{}
	require.Equal(t, `ALTER TABLE "ks"."events" ADD "payload" text`,
		d.AddColumnSQL("KS.Events", schema.Column{Name: "payload", DataType: schema.TypeJSON}))
	require.False(t, d.SupportsAlterType())
}
