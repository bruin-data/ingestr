package intercom

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"intercom"},
		func() interface{} { return NewIntercomSource() },
	)
}
