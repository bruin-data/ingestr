package clevertap

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"clevertap"},
		func() interface{} { return NewCleverTapDestination() },
	)
}
