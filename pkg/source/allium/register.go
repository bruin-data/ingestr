package allium

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"allium"},
		func() interface{} { return NewAlliumSource() },
	)
}
