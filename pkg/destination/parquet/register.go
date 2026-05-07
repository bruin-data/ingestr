package parquet

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"parquet"},
		func() interface{} { return NewParquetDestination() },
	)
}
