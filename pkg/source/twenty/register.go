package twenty

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"twenty"},
		func() interface{} { return NewTwentySource() },
	)
}
