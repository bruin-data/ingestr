package googleads

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"googleads"},
		func() interface{} { return NewGoogleAdsSource() },
	)
}
