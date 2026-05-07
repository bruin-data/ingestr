package athena

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"athena"},
		func() interface{} { return NewAthenaSource() },
	)
}
