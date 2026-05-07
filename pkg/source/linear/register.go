package linear

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"linear"},
		func() interface{} { return NewLinearSource() },
	)
}
