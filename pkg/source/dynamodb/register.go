package dynamodb

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"dynamodb"},
		func() interface{} { return NewDynamoDBSource() },
	)
}
