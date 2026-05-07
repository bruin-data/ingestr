package dynamodb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"dynamodb"},
		func() interface{} { return NewDynamoDBDestination() },
	)
}
