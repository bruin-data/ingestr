package linkedinads

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"linkedinads"},
		func() interface{} { return NewLinkedInAdsSource() },
	)
}
