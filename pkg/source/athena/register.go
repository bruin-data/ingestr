package athena

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"athena"},
		func() interface{} { return NewAthenaSource() },
	)
}
