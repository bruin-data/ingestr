package lumify

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"lumify"},
		func() interface{} { return NewLumifySource() },
	)
}
