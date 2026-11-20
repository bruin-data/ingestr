package breezyhr

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"breezyhr"},
		func() interface{} { return NewBreezyHRSource() },
	)
}
