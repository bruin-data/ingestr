package plusvibeai

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"plusvibeai"},
		func() interface{} { return NewPlusVibeAI() },
	)
}
