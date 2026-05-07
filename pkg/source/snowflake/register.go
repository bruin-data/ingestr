package snowflake

import (
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
)

func init() {
	registry.RegisterSource(
		[]string{"snowflake"},
		func() interface{} { return adbc.NewADBCSource(NewDialect()) },
	)
}
