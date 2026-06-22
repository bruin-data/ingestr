package mongodb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mongodb", "mongodb+srv"},
		func() interface{} { return NewMongoDBSource() },
	)
	registry.RegisterSource(
		[]string{"mongodb+cdc", "mongodb+srv+cdc"},
		func() interface{} { return NewMongoDBCDCSource() },
	)
}
