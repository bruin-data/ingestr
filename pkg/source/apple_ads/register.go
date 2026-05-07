package appleads

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"appleads"},
		func() interface{} { return NewAppleAdsSource() },
	)
}
