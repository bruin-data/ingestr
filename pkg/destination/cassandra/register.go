package cassandra

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"cassandra"},
		func() interface{} { return NewCassandraDestination() },
	)
}
