package personio

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"personio"},
		func() interface{} { return NewPersonioSource() },
	)
}
