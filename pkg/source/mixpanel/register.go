package mixpanel

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mixpanel"},
		func() interface{} { return NewMixpanelSource() },
	)
}
