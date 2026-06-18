package polymarket

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"polymarket"},
		func() interface{} { return NewSource() },
	)
}
