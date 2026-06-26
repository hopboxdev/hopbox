package main

import (
	"context"
	"sync"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

// memStore is boxd's in-memory box.Store. Front-door boxes are ephemeral (reaped
// on disconnect), so in-memory is a fit for the box product's core use case; a
// persistent box-native store is a follow-up. It also serves the agent hub's
// token->box lookup.
type memStore struct {
	mu      sync.RWMutex
	byID    map[string]*box.Box
	byName  map[string]*box.Box // key: tenant/name
	byToken map[string]*box.Box
}

var _ box.Store = (*memStore)(nil)

func newMemStore() *memStore {
	return &memStore{byID: map[string]*box.Box{}, byName: map[string]*box.Box{}, byToken: map[string]*box.Box{}}
}

func (s *memStore) index(b *box.Box) {
	s.byID[b.ID] = b
	s.byName[b.TenantID+"/"+b.Name] = b
	if b.BootstrapToken != "" {
		s.byToken[b.BootstrapToken] = b
	}
}

func (s *memStore) Get(_ context.Context, _ /*tenant*/, id string) (*box.Box, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if b, ok := s.byID[id]; ok {
		c := *b
		return &c, nil
	}
	return nil, box.ErrNotFound
}

func (s *memStore) GetByName(_ context.Context, tenant, name string) (*box.Box, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if b, ok := s.byName[tenant+"/"+name]; ok {
		c := *b
		return &c, nil
	}
	return nil, box.ErrNotFound
}

// GetByToken backs the agent hub's resolver (not part of box.Store).
func (s *memStore) GetByToken(token string) (*box.Box, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if b, ok := s.byToken[token]; ok {
		c := *b
		return &c, nil
	}
	return nil, box.ErrNotFound
}

// List returns all boxes when tenant is "" (the reconciler sweep), else by tenant.
func (s *memStore) List(_ context.Context, tenant string) ([]*box.Box, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*box.Box
	for _, b := range s.byID {
		if tenant == "" || b.TenantID == tenant {
			c := *b
			out = append(out, &c)
		}
	}
	return out, nil
}

func (s *memStore) Create(_ context.Context, b *box.Box) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *b
	s.index(&c)
	return nil
}

func (s *memStore) Update(_ context.Context, b *box.Box) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *b
	s.index(&c)
	return nil
}

func (s *memStore) Delete(_ context.Context, _ /*tenant*/, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.byID[id]; ok {
		delete(s.byID, id)
		delete(s.byName, b.TenantID+"/"+b.Name)
		delete(s.byToken, b.BootstrapToken)
	}
	return nil
}
