package uri

import (
	"fmt"

	"github.com/bruin-data/ingestr/internal/registry"
	_ "github.com/bruin-data/ingestr/internal/registry/imports"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// DefaultRegistry provides access to registered sources and destinations.
// Sources and destinations register themselves via init() functions in their
// respective register.go files, which are triggered by the blank import of
// internal/registry/imports.
var DefaultRegistry = &registryAdapter{}

type registryAdapter struct{}

func (r *registryAdapter) GetSource(uri string) (source.Source, error) {
	parsed, err := Parse(uri)
	if err != nil {
		return nil, err
	}

	constructor, err := registry.Default.GetSourceConstructor(NormalizeScheme(parsed.Scheme))
	if err != nil {
		return nil, err
	}

	src, ok := constructor().(source.Source)
	if !ok {
		return nil, fmt.Errorf("invalid source constructor for scheme: %s", parsed.Scheme)
	}

	return src, nil
}

func (r *registryAdapter) GetDestination(uri string) (destination.Destination, error) {
	parsed, err := Parse(uri)
	if err != nil {
		return nil, err
	}

	constructor, err := registry.Default.GetDestinationConstructor(NormalizeScheme(parsed.Scheme))
	if err != nil {
		return nil, err
	}

	dest, ok := constructor().(destination.Destination)
	if !ok {
		return nil, fmt.Errorf("invalid destination constructor for scheme: %s", parsed.Scheme)
	}

	return dest, nil
}
