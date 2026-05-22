package granola

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"granola"},
		func() interface{} { return NewGranolaSource() },
	)
}
