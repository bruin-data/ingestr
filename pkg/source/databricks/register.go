package databricks

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"databricks"},
		func() interface{} { return NewDatabricksSource() },
	)
}
