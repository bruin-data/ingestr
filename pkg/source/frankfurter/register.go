package frankfurter

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"frankfurter"},
		func() interface{} { return NewFrankfurterSource() },
	)
}
