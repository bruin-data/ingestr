package wise

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"wise"},
		func() interface{} { return NewWiseSource() },
	)
}
