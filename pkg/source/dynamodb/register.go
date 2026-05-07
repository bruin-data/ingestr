package dynamodb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"dynamodb"},
		func() interface{} { return NewDynamoDBSource() },
	)
}
