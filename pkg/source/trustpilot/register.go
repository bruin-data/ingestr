package trustpilot

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"trustpilot"},
		func() interface{} { return NewTrustpilotSource() },
	)
}
