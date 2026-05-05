package spanner

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"spanner"},
		func() interface{} { return NewSpannerSource() },
	)
}
