package attio

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"attio"},
		func() interface{} { return NewAttioSource() },
	)
}
