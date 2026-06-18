package wistia

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"wistia"},
		func() interface{} { return NewWistiaSource() },
	)
}
