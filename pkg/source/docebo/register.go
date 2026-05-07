package docebo

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"docebo"},
		func() interface{} { return NewDoceboSource() },
	)
}
