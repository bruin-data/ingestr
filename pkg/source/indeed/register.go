package indeed

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"indeed"},
		func() interface{} { return NewIndeedSource() },
	)
}
