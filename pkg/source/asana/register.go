package asana

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"asana"},
		func() interface{} { return NewAsanaSource() },
	)
}
