package bigquery

import (
	"github.com/bruin-data/ingestr/internal/registry"
)

func init() {
	registry.RegisterSource(
		[]string{"bigquery"},
		func() interface{} { return NewSource() },
	)
}
