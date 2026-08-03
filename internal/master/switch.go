package master

import (
	"net/http"
	"strings"
)

// handleSwitch 校验 Agent 在线后写入 mp_node Cookie，并重定向到 1Panel 入口。
func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	_, ok := s.reg.Get(id)
	if !ok {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    id,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	target := "/"
	if s.Entrance != "" {
		target = "/" + strings.TrimPrefix(s.Entrance, "/")
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleLocal 清除 mp_node，切回本机 1Panel 反代。
func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
	})
	target := "/"
	if s.Entrance != "" {
		target = "/" + strings.TrimPrefix(s.Entrance, "/")
	}
	http.Redirect(w, r, target, http.StatusFound)
}
