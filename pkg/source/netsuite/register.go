package netsuite

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"netsuite", "netsuite+odbc"},
		func() interface{} { return NewNetSuiteSource() },
	)
}
