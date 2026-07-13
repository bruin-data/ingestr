package amplitude

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"amplitude"},
		func() interface{} { return NewAmplitudeSource() },
	)
}
