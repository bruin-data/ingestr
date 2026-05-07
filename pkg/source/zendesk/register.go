package zendesk

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"zendesk"},
		func() interface{} { return NewZendeskSource() },
	)
}
