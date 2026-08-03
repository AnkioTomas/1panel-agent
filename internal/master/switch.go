package master

import (
	"net/http"
	"strings"
)

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
