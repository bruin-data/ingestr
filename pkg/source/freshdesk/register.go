package freshdesk

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"freshdesk"},
		func() interface{} { return NewFreshdeskSource() },
	)
}
