package clevertap

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"clevertap"},
		func() interface{} { return NewCleverTapSource() },
	)
}
