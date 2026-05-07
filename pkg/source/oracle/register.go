package oracle

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"oracle", "oracle+cx_oracle"},
		func() interface{} { return NewOracleSource() },
	)
}
