package snapchatads

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"snapchatads"},
		func() interface{} { return NewSnapchatAdsSource() },
	)
}
