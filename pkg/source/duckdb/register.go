package duckdb

import (
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
)

func init() {
	registry.RegisterSource(
		[]string{"duckdb", "motherduck", "md"},
		func() interface{} { return adbc.NewADBCSource(NewDialect()) },
	)
	registry.RegisterSource(
		[]string{"ducklake"},
		func() interface{} { return adbc.NewADBCSource(NewDuckLakeDialect()) },
	)
}
