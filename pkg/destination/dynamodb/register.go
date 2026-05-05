package dynamodb

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"dynamodb"},
		func() interface{} { return NewDynamoDBDestination() },
	)
}
