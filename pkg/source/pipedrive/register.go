package pipedrive

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"pipedrive"},
		func() any { return NewPipedriveSource() },
	)
}
