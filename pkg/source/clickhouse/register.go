package clickhouse

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"clickhouse"},
		func() interface{} { return NewClickHouseSource() },
	)
}
