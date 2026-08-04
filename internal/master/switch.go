package master

import (
	"net/http"
)

// handleSwitch 校验 Agent 在线后：暂存本机面板会话到 Master 内存、清浏览器面板 Cookie、
// 设置 mp_node 并重定向。Master 不预热、不持有 Agent Cookie。
func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	if _, ok := s.reg.Get(id); !ok {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}

	// 只在离开主节点时暂存。agent→agent 时浏览器里是远端会话，
	// 若覆盖 stash 会把本机会话丢掉，切回主节点就被迫重新登录。
	if c, err := r.Cookie("mp_node"); err != nil || c.Value == "" {
		s.stashLocalSession(collectPanelSessionCookies(r))
	}
	expirePanelSessionCookies(w)

	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    id,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLocal 切回主节点：清 mp_node，从 Master 内存恢复本机面板会话。
// 必须在 ensureMPAuth 之前调用——此时浏览器里是远端 psession，本机探测会失败。
func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request) {
	s.restoreLocalAndRedirect(w, r, "/")
}

// restoreLocalAndRedirect 清 mp_node、写回暂存的本机会话，再 302。
// 回主节点必须先 Set-Cookie 再跳转：同请求反代本机仍带着远端 Cookie，会直接打进登录页。
func (s *Server) restoreLocalAndRedirect(w http.ResponseWriter, r *http.Request, target string) {
	if target == "" {
		target = "/"
	}
	clearNodeCookie(w)
	writePanelSessionCookies(w, s.takeLocalSession())
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) stashLocalSession(cookies []*http.Cookie) {
	s.localSessMu.Lock()
	defer s.localSessMu.Unlock()
	if len(cookies) == 0 {
		return
	}
	s.localSessCookies = cookies
}

func (s *Server) takeLocalSession() []*http.Cookie {
	s.localSessMu.Lock()
	defer s.localSessMu.Unlock()
	out := s.localSessCookies
	s.localSessCookies = nil
	return out
}
