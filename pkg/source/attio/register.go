package attio

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"attio"},
		func() interface{} { return NewAttioSource() },
	)
}
