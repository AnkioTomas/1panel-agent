package master

import (
	"sync"

	"github.com/xtaci/smux"
)

// AgentInfo 是在线 Agent 的展示元数据（不含隧道句柄）。
type AgentInfo struct {
	ID           string
	Hostname     string
	PanelURL     string
	RemoteIP     string
	PanelVersion string
}

// Session 绑定一个在线 Agent 的信息与其 smux 会话。
type Session struct {
	Info AgentInfo
	Mux  *smux.Session
}

// Registry 按 Agent ID 管理在线 Session；并发安全。
type Registry struct {
	mu   sync.RWMutex
	byID map[string]*Session
}

// NewRegistry 创建空的 Agent 注册表。
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Session)}
}

// Put 登记或替换 Session；若存在未关闭的旧会话则关闭并返回 replaced=true。
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

// Remove 仅在当前登记的 Mux 与参数一致（或 mux 为 nil）时删除，避免误删新连接。
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

// Get 返回仍可用（Mux 未关闭）的 Session。
func (r *Registry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[id]
	if !ok || s.Mux == nil || s.Mux.IsClosed() {
		return nil, false
	}
	return s, true
}

// List 返回当前在线 Agent 的快照列表。
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
