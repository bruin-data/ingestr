package allium

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"allium"},
		func() interface{} { return NewAlliumSource() },
	)
}
