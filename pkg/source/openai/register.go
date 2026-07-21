package openai

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"openai"},
		func() interface{} { return NewOpenAISource() },
	)
}
