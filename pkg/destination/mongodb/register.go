package mongodb

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"mongodb", "mongodb+srv"},
		func() interface{} { return NewMongoDBDestination() },
	)
}
