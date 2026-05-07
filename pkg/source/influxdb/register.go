package influxdb

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"influxdb"},
		func() interface{} { return NewInfluxDBSource() },
	)
}
