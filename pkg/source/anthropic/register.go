package anthropic

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"anthropic"},
		func() interface{} { return NewAnthropicSource() },
	)
}
