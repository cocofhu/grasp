package chatsession

import "sync"

// Idler is a registry value that can report it may be discarded.
type Idler interface {
	Idle() bool
}

// Registry maps chat keys to their sessions.
type Registry[V Idler] struct {
	mu sync.Mutex
	m  map[string]V
}

// GetOrCreate returns the session for key, building it with create when absent.
// create runs under the registry lock and must not call back into it.
func (r *Registry[V]) GetOrCreate(key string, create func() V) V {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.m[key]; ok {
		return v
	}
	if r.m == nil {
		r.m = map[string]V{}
	}
	v := create()
	r.m[key] = v
	return v
}

// Get returns the session for key.
func (r *Registry[V]) Get(key string) (V, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.m[key]
	return v, ok
}

// DropIfIdle forgets key when its session is idle.
func (r *Registry[V]) DropIfIdle(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.m[key]; ok && v.Idle() {
		delete(r.m, key)
	}
}

// Range calls fn for every session until fn returns false. fn runs under the
// registry lock and must not call back into it.
func (r *Registry[V]) Range(fn func(key string, v V) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range r.m {
		if !fn(k, v) {
			return
		}
	}
}
