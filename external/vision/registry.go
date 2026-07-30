package vision

import (
	"sort"
	"strings"
	"sync"
)

// Registration describes a reusable vision example that can be advertised or instantiated.
type Registration struct {
	Key         string
	Name        string
	Kind        string
	Description string
	Build       func() *Vision
}

// NewVision returns a model for this registration.
func (r Registration) NewVision() *Vision {
	if r.Build != nil {
		return r.Build()
	}

	return &Vision{
		Name:        normaliseVisionName(r.Name),
		Kind:        normaliseVisionKind(r.Kind),
		Description: strings.TrimSpace(r.Description),
		Status:      VisionStatusDraft,
	}
}

// Registry stores vision registrations by key.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]Registration
}

// NewRegistry creates a registry and registers the supplied entries.
func NewRegistry(entries ...Registration) (*Registry, error) {
	registry := &Registry{entries: map[string]Registration{}}
	for _, entry := range entries {
		if err := registry.Register(entry); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

// MustRegistry creates a registry and panics on invalid registrations.
func MustRegistry(entries ...Registration) *Registry {
	registry, err := NewRegistry(entries...)
	if err != nil {
		panic(err)
	}

	return registry
}

// Register adds a registration to the registry.
func (r *Registry) Register(entry Registration) error {
	if r == nil {
		return ErrVisionRegistrationNotFound
	}

	entry.Key = normaliseRegistrationKey(entry.Key)
	if entry.Key == "" {
		return ErrVisionRegistrationKeyMissing
	}
	entry.Name = normaliseVisionName(entry.Name)
	if entry.Name == "" {
		return ErrVisionNameIsRequired
	}
	entry.Kind = normaliseVisionKind(entry.Kind)
	if entry.Kind == "" {
		return ErrVisionKindIsRequired
	}
	entry.Description = strings.TrimSpace(entry.Description)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		r.entries = map[string]Registration{}
	}

	if _, exists := r.entries[entry.Key]; exists {
		return ErrVisionRegistrationConflict
	}

	r.entries[entry.Key] = entry
	return nil
}

// Get returns a registration by key.
func (r *Registry) Get(key string) (Registration, bool) {
	if r == nil {
		return Registration{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[normaliseRegistrationKey(key)]
	return entry, exists
}

// MustGet returns a registration by key or an error when it is missing.
func (r *Registry) MustGet(key string) (Registration, error) {
	entry, exists := r.Get(key)
	if !exists {
		return Registration{}, ErrVisionRegistrationNotFound
	}

	return entry, nil
}

// List returns registrations ordered by key.
func (r *Registry) List() []Registration {
	if r == nil {
		return []Registration{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.entries))
	for key := range r.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]Registration, 0, len(keys))
	for _, key := range keys {
		result = append(result, r.entries[key])
	}

	return result
}

func normaliseRegistrationKey(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
