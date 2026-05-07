package snowflake

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"snowflake"},
		func() interface{} { return NewSnowflakeDestination() },
	)
}
