package snowflake

import (
	"github.com/bruin-data/gong/internal/registry"
	"github.com/bruin-data/gong/pkg/source/adbc"
)

func init() {
	registry.RegisterSource(
		[]string{"snowflake"},
		func() interface{} { return adbc.NewADBCSource(NewDialect()) },
	)
}
