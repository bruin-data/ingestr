package snapchatads

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"snapchatads"},
		func() interface{} { return NewSnapchatAdsSource() },
	)
}
