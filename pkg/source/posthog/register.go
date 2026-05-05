package posthog

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"posthog"},
		func() interface{} { return NewPostHogSource() },
	)
}
