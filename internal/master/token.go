package master

import (
	"encoding/json"
	"net/http"

	"1panel-agent/internal/config"
)

func (s *Server) currentToken() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.Token
}

func (s *Server) setToken(tok string) {
	s.tokenMu.Lock()
	s.Token = tok
	s.tokenMu.Unlock()
}

func (s *Server) tokenOK(got string) bool {
	return got != "" && got == s.currentToken()
}

// RotateToken generates a new token, persists it, and drops current agent sessions.
func (s *Server) RotateToken() (string, error) {
	tok, err := config.GenerateToken()
	if err != nil {
		return "", err
	}
	st, err := config.LoadMasterOrEmpty()
	if err != nil {
		return "", err
	}
	st.Token = tok
	if err := config.SaveMaster(st); err != nil {
		return "", err
	}
	s.setToken(tok)
	// Force agents to reconnect with the new token (after reinstall/re-register).
	for _, a := range s.reg.List() {
		if sess, ok := s.reg.Get(a.ID); ok && sess.Mux != nil {
			_ = sess.Mux.Close()
		}
	}
	return tok, nil
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tok, err := s.RotateToken()
	if err != nil {
		http.Error(w, "rotate failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Re-issue mp_auth under the new HMAC secret so the UI session survives.
	s.issueAuthCookie(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":   tok,
		"install": s.InstallCommand(r),
	})
}
