package adjust

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"adjust"},
		func() interface{} { return NewAdjustSource() },
	)
}
