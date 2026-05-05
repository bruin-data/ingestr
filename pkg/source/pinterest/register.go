package pinterest

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"pinterest"},
		func() interface{} { return NewPinterestSource() },
	)
}
