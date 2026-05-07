package salesforce

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"salesforce"},
		func() interface{} { return NewSalesforceSource() },
	)
}
