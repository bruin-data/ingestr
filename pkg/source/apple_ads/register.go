package appleads

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"appleads"},
		func() interface{} { return NewAppleAdsSource() },
	)
}
