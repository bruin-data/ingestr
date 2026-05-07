package kinesis

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"kinesis"},
		func() interface{} { return NewKinesisSource() },
	)
}
