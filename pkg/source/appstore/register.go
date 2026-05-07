package appstore

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"appstore"},
		func() interface{} { return NewAppStoreSource() },
	)
}
