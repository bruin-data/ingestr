package gorgias

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"gorgias"},
		func() interface{} { return NewGorgiasSource() },
	)
}
