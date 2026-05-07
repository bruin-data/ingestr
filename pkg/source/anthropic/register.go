package anthropic

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"anthropic"},
		func() interface{} { return NewAnthropicSource() },
	)
}
