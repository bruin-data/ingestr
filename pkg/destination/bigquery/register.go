package bigquery

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"bigquery"},
		func() interface{} { return NewBigQueryDestination() },
	)
}
