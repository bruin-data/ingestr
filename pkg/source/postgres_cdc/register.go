package postgres_cdc

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"postgres+cdc", "postgresql+cdc"},
		func() interface{} { return NewPostgresCDCSource() },
	)
}
