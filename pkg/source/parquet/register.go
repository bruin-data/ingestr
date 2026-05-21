package parquet

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"parquet"},
		func() interface{} { return NewParquetSource() },
	)
}
