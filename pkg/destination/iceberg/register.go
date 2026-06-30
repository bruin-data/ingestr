package iceberg

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"iceberg", "iceberg+rest", "iceberg+glue", "iceberg+hive", "iceberg+hadoop", "iceberg+sql", "iceberg+sqlite", "iceberg+postgres"},
		func() interface{} { return NewDestination() },
	)
}
