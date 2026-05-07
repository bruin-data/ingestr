package tiktokads

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"tiktok"},
		func() interface{} { return NewTiktokAdsSource() },
	)
}
