package eventhubs

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"eventhubs", "eventhub", "azure-event-hubs", "azureeventhubs"},
		func() interface{} { return NewEventHubsSource() },
	)
}
