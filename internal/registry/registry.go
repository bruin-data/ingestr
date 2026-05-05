package registry

import (
	"fmt"
	"sync"
)

type (
	SourceConstructor      func() interface{}
	DestinationConstructor func() interface{}
)

type Registry struct {
	mu           sync.RWMutex
	sources      map[string]SourceConstructor
	destinations map[string]DestinationConstructor
}

func NewRegistry() *Registry {
	return &Registry{
		sources:      make(map[string]SourceConstructor),
		destinations: make(map[string]DestinationConstructor),
	}
}

var Default = NewRegistry()

func (r *Registry) RegisterSource(schemes []string, constructor SourceConstructor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, scheme := range schemes {
		r.sources[normalizeScheme(scheme)] = constructor
	}
}

func (r *Registry) RegisterDestination(schemes []string, constructor DestinationConstructor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, scheme := range schemes {
		r.destinations[normalizeScheme(scheme)] = constructor
	}
}

func (r *Registry) GetSourceConstructor(scheme string) (SourceConstructor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructor, ok := r.sources[normalizeScheme(scheme)]
	if !ok {
		return nil, fmt.Errorf("unsupported source scheme: %s", scheme)
	}
	return constructor, nil
}

func (r *Registry) GetDestinationConstructor(scheme string) (DestinationConstructor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructor, ok := r.destinations[normalizeScheme(scheme)]
	if !ok {
		return nil, fmt.Errorf("unsupported destination scheme: %s", scheme)
	}
	return constructor, nil
}

func RegisterSource(schemes []string, constructor SourceConstructor) {
	Default.RegisterSource(schemes, constructor)
}

func RegisterDestination(schemes []string, constructor DestinationConstructor) {
	Default.RegisterDestination(schemes, constructor)
}

func normalizeScheme(scheme string) string {
	return scheme
}
