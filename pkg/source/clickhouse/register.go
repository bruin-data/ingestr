package clickhouse

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"clickhouse"},
		func() interface{} { return NewClickHouseSource() },
	)
}
