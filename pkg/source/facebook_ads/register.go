package facebook_ads

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"facebookads"},
		func() interface{} { return NewFacebookAdsSource() },
	)
}
