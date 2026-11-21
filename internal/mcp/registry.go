package mcp

import (
	"context"
	"sync"
)

// Registry keeps one live connection per enabled server, connecting lazily
// and reconnecting after failures or config changes.
type Registry struct {
	store *Store
	mu    sync.Mutex
	conns map[string]*Client
}

// NewRegistry wraps a store with a connection cache.
func NewRegistry(store *Store) *Registry {
	return &Registry{store: store, conns: map[string]*Client{}}
}

// Client returns a connected client for the server id, spawning it if needed.
func (r *Registry) Client(ctx context.Context, id string) (*Client, error) {
	r.mu.Lock()
	if c, ok := r.conns[id]; ok {
		r.mu.Unlock()
		return c, nil
	}
	r.mu.Unlock()

	srv, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	c, err := Connect(ctx, *srv)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	// Another caller may have connected meanwhile; keep one.
	if old, ok := r.conns[id]; ok {
		r.mu.Unlock()
		c.Close()
		return old, nil
	}
	r.conns[id] = c
	r.mu.Unlock()
	return c, nil
}

// Invalidate drops the cached connection for a server (config changed).
func (r *Registry) Invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.conns[id]; ok {
		c.Close()
		delete(r.conns, id)
	}
}

// CloseAll terminates every connection.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.conns {
		c.Close()
		delete(r.conns, id)
	}
}
