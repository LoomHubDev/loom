package adapter

import "sync"

// AdapterRegistry holds registered space adapters and provides lookup methods.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]SpaceAdapter
}

// NewRegistry creates a new AdapterRegistry with all built-in adapters registered.
func NewRegistry() *AdapterRegistry {
	r := &AdapterRegistry{
		adapters: make(map[string]SpaceAdapter),
	}
	r.Register(&CodeAdapter{})
	r.Register(&DocsAdapter{})
	r.Register(&FilesystemAdapter{})
	return r
}

// Register adds an adapter to the registry. If an adapter with the same ID
// already exists, it is replaced.
func (r *AdapterRegistry) Register(a SpaceAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.ID()] = a
}

// Get returns the adapter with the given ID, or nil if not found.
func (r *AdapterRegistry) Get(id string) SpaceAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[id]
}

// ForSpace returns the adapter for the given adapter name. If the name is not
// found, it falls back to the filesystem adapter.
func (r *AdapterRegistry) ForSpace(adapterName string) SpaceAdapter {
	if a := r.Get(adapterName); a != nil {
		return a
	}
	return r.Get("filesystem")
}

// DetectSpaces runs Detect on all registered adapters against projectPath and
// returns a map of adapterID to adapterID for each adapter that detects a match.
func (r *AdapterRegistry) DetectSpaces(projectPath string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string)
	for id, a := range r.adapters {
		if a.Detect(projectPath) {
			result[id] = id
		}
	}
	return result
}
