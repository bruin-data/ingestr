package cassandra

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"cassandra"},
		func() interface{} { return NewCassandraSource() },
	)
}
