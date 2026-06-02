package cassandrautil

import (
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	cfg, err := ParseURI("cassandra://user:pass@host1/Analytics?hosts=host1:9142,host2&consistency=local_quorum&page_size=123&timeout=5s&connect_timeout=2s&disable_initial_host_lookup=true&replication_factor=3")
	require.NoError(t, err)
	require.Equal(t, []string{"host1", "host2"}, cfg.Hosts)
	require.Equal(t, 9142, cfg.Port)
	require.Equal(t, "analytics", cfg.Keyspace)
	require.Equal(t, "user", cfg.Username)
	require.Equal(t, "pass", cfg.Password)
	require.Equal(t, gocql.LocalQuorum, cfg.Consistency)
	require.Equal(t, 123, cfg.PageSize)
	require.Equal(t, 5*time.Second, cfg.Timeout)
	require.Equal(t, 2*time.Second, cfg.ConnectTimeout)
	require.True(t, cfg.DisableInitialHostLookup)
	require.Equal(t, 3, cfg.ReplicationFactor)
}

func TestParseURIHostsParam(t *testing.T) {
	cfg, err := ParseURI("cassandra://seed/keyspace?hosts=host1,host2:9043")
	require.NoError(t, err)
	require.Equal(t, []string{"host1", "host2"}, cfg.Hosts)
	require.Equal(t, 9043, cfg.Port)
}

func TestResolveTableName(t *testing.T) {
	keyspace, table, err := ResolveTableName("DefaultKS", "Users")
	require.NoError(t, err)
	require.Equal(t, "defaultks", keyspace)
	require.Equal(t, "users", table)

	keyspace, table, err = ResolveTableName("", `"CamelKS"."CamelTable"`)
	require.NoError(t, err)
	require.Equal(t, "CamelKS", keyspace)
	require.Equal(t, "CamelTable", table)
}

func TestMapCassandraToDataType(t *testing.T) {
	tests := []struct {
		cql       string
		want      schema.DataType
		wantArray schema.DataType
	}{
		{cql: "int", want: schema.TypeInt32},
		{cql: "timestamp", want: schema.TypeTimestampTZ},
		{cql: "uuid", want: schema.TypeUUID},
		{cql: "list<text>", want: schema.TypeArray, wantArray: schema.TypeString},
		{cql: "frozen<set<int>>", want: schema.TypeArray, wantArray: schema.TypeInt32},
		{cql: "map<text,int>", want: schema.TypeJSON},
	}

	for _, tt := range tests {
		got, _, _, arrayType := MapCassandraToDataType(tt.cql)
		require.Equal(t, tt.want, got, tt.cql)
		require.Equal(t, tt.wantArray, arrayType, tt.cql)
	}
}

func TestMapDataTypeToCassandra(t *testing.T) {
	require.Equal(t, "uuid", MapDataTypeToCassandra(schema.Column{DataType: schema.TypeUUID}))
	require.Equal(t, "timestamp", MapDataTypeToCassandra(schema.Column{DataType: schema.TypeTimestampTZ}))
	require.Equal(t, "list<int>", MapDataTypeToCassandra(schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt32}))
}
