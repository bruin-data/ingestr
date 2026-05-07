package facebook_ads

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"facebookads"},
		func() interface{} { return NewFacebookAdsSource() },
	)
}
