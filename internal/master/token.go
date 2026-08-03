package master

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"1panel-agent/internal/config"
)

// currentToken 并发安全读取当前安装/注册 Token。
func (s *Server) currentToken() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.Token
}

// setToken 并发安全更新内存中的 Token（调用方负责落盘）。
func (s *Server) setToken(tok string) {
	s.tokenMu.Lock()
	s.Token = tok
	s.tokenMu.Unlock()
}

// VerifyToken 严格校验带时间戳与 HMAC-SHA256 签名的请求，防止重放攻击（±5分钟衰减期）。
func (s *Server) VerifyToken(timestampStr, sign string) bool {
	secret := s.currentToken()
	if !config.SignOK(secret, timestampStr, sign) {
		return false
	}
	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	return ts >= now-300 && ts <= now+300
}

// RotateToken 生成新 Token、落盘，并断开现有 Agent 会话迫使其用新密钥重连。
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
	for _, a := range s.reg.List() {
		if sess, ok := s.reg.Get(a.ID); ok && sess.Mux != nil {
			_ = sess.Mux.Close()
		}
	}
	return tok, nil
}

// handleRotateToken 处理 POST /__mp/api/rotate-token，返回新 Token 与安装命令。
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
	s.issueAuthCookie(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":   tok,
		"install": s.InstallCommand(r),
	})
}
