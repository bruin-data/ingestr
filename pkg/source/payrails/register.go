package payrails

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"payrails"},
		func() interface{} { return NewPayrailsSource() },
	)
}
