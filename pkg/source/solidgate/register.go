package solidgate

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"solidgate"},
		func() interface{} { return NewSolidGateSource() },
	)
}
