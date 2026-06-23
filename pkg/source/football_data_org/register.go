package football_data_org

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"footballdata"},
		func() interface{} { return NewFootballDataOrgSource() },
	)
}
