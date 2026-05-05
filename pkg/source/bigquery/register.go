package bigquery

import (
	"github.com/bruin-data/gong/internal/registry"
	"github.com/bruin-data/gong/pkg/source/adbc"
)

func init() {
	registry.RegisterSource(
		[]string{"bigquery"},
		func() interface{} { return adbc.NewADBCSource(NewDialect()) },
	)
}
