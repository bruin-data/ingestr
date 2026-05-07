package mixpanel

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mixpanel"},
		func() interface{} { return NewMixpanelSource() },
	)
}
