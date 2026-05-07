package isoc_pulse

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"isoc-pulse"},
		func() interface{} { return NewIsocPulseSource() },
	)
}
