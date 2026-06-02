package cassandra

import (
	"github.com/bruin-data/ingestr/internal/cassandrautil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapDataTypeToCassandra(col schema.Column) string {
	return cassandrautil.MapDataTypeToCassandra(col)
}
