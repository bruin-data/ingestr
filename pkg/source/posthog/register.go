package posthog

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"posthog"},
		func() interface{} { return NewPostHogSource() },
	)
}
