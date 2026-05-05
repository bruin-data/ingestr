package kinesis

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"kinesis"},
		func() interface{} { return NewKinesisSource() },
	)
}
