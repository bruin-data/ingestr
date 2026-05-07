package mongodb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mongodb", "mongodb+srv"},
		func() interface{} { return NewMongoDBSource() },
	)
}
