package hostaway

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"hostaway"},
		func() interface{} { return NewHostawaySource() },
	)
}
