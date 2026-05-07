package databricks

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"databricks"},
		func() interface{} { return NewDatabricksDestination() },
	)
}
