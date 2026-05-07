package gorgias

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"gorgias"},
		func() interface{} { return NewGorgiasSource() },
	)
}
