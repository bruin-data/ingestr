package databricks

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"databricks"},
		func() interface{} { return NewDatabricksDestination() },
	)
}
