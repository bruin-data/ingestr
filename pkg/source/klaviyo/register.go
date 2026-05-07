package klaviyo

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"klaviyo"},
		func() interface{} { return NewKlaviyoSource() },
	)
}
