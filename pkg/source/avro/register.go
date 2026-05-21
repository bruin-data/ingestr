package avro

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"avro"},
		func() interface{} { return NewAvroSource() },
	)
}
