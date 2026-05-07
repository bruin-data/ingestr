package appstore

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"appstore"},
		func() interface{} { return NewAppStoreSource() },
	)
}
