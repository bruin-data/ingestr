package tiktokads

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"tiktok"},
		func() interface{} { return NewTiktokAdsSource() },
	)
}
