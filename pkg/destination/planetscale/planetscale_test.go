package planetscale

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/assert"
)

func TestDestinationImplementsDestination(t *testing.T) {
	var _ destination.Destination = NewDestination()
}

func TestDestinationSchemes(t *testing.T) {
	dest := NewDestination()

	assert.Equal(t, []string{"ps_mysql"}, dest.Schemes())
	assert.Equal(t, "ps_mysql", dest.GetScheme())
}
