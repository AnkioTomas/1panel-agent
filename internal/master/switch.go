package master

import (
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"time"

	"1panel-agent/internal/panel"
)

func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	sess, ok := s.reg.Get(id)
	if !ok {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}
	if s.PanelUser == "" || s.PanelPass == "" {
		http.Error(w, "master panel user/password not configured", http.StatusInternalServerError)
		return
	}

	entrance := s.Entrance
	client := &http.Client{
		Transport: &tunnelTransport{mux: sess.Mux},
		Timeout:   45 * time.Second,
	}
	res, err := panel.LoginWithClient(client, "http://agent.panel", entrance, s.PanelUser, s.PanelPass, "")
	if err != nil {
		log.Printf("switch login %s: %v", id, err)
		http.Error(w, "auto login failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    id,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	seen := map[string]bool{}
	for _, c := range res.Cookies {
		seen[c.Name] = true
		http.SetCookie(w, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     "/",
			HttpOnly: c.HttpOnly,
			Secure:   false,
			SameSite: http.SameSiteLaxMode,
		})
	}
	if entrance != "" && !seen["SecurityEntrance"] {
		http.SetCookie(w, &http.Cookie{
			Name:     "SecurityEntrance",
			Value:    base64.StdEncoding.EncodeToString([]byte(entrance)),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	target := "/"
	if entrance != "" {
		target = "/" + strings.TrimPrefix(entrance, "/")
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
