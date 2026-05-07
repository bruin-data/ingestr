package g2

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"g2"},
		func() interface{} { return NewG2Source() },
	)
}
