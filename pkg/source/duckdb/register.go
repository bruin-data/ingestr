package duckdb

import (
	"github.com/bruin-data/gong/internal/registry"
	"github.com/bruin-data/gong/pkg/source/adbc"
)

func init() {
	registry.RegisterSource(
		[]string{"duckdb", "motherduck", "md"},
		func() interface{} { return adbc.NewADBCSource(NewDialect()) },
	)
}
