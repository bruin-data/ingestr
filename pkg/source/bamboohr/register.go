package bamboohr

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"bamboohr"},
		func() interface{} { return NewBambooHRSource() },
	)
}
