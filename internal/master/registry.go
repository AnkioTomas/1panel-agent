package master

import (
	"sync"

	"github.com/xtaci/smux"
)

type AgentInfo struct {
	ID       string
	Hostname string
	PanelURL string
}

type Session struct {
	Info AgentInfo
	Mux  *smux.Session
}

type Registry struct {
	mu   sync.RWMutex
	byID map[string]*Session
}

func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Session)}
}

func (r *Registry) Put(s *Session) (replaced bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.byID[s.Info.ID]
	if ok && old.Mux != nil && !old.Mux.IsClosed() {
		_ = old.Mux.Close()
		replaced = true
	}
	r.byID[s.Info.ID] = s
	return replaced
}

func (r *Registry) Remove(id string, mux *smux.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.byID[id]
	if !ok {
		return
	}
	if mux != nil && cur.Mux != mux {
		return
	}
	delete(r.byID, id)
}

func (r *Registry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[id]
	if !ok || s.Mux == nil || s.Mux.IsClosed() {
		return nil, false
	}
	return s, true
}

func (r *Registry) List() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentInfo, 0, len(r.byID))
	for _, s := range r.byID {
		if s.Mux == nil || s.Mux.IsClosed() {
			continue
		}
		out = append(out, s.Info)
	}
	return out
}
