package master

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"1panel-agent/internal/config"
)

// authCookie 是 Master 管理页会话 Cookie 名。
const (
	authCookie = "mp_auth"
)

// issueAuthCookie 确认 1Panel 授权通过后生成全新 sessionSecret 并覆盖签发（单点 Session 顶掉旧会话）。
func (s *Server) issueAuthCookie(w http.ResponseWriter) {
	sec, err := config.GenerateToken()
	if err != nil {
		return
	}
	s.tokenMu.Lock()
	s.sessionSecret = sec
	s.tokenMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    sec,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// validAuthCookie 验证 mp_auth Cookie：长度必须匹配且与最新内存 sessionSecret 时序一致。
func (s *Server) validAuthCookie(r *http.Request) bool {
	c, err := r.Cookie(authCookie)
	if err != nil || len(c.Value) != 32 {
		return false
	}
	s.tokenMu.RLock()
	secret := s.sessionSecret
	s.tokenMu.RUnlock()

	if len(secret) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(secret)) == 1
}

// localPanelLoggedIn 代理向本地 1Panel 接口确认访问者是否已拥有合法的 1Panel 登录态。
func (s *Server) localPanelLoggedIn(r *http.Request) bool {
	return s.localPanelCodeOK(r.Header.Get("Cookie"))
}

func (s *Server) localPanelCodeOK(cookieHeader string) bool {
	if s.LocalPanel == "" || cookieHeader == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(s.LocalPanel, "/")+"/api/v2/dashboard/base/os", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Cookie", cookieHeader)
	if s.Entrance != "" {
		req.Header.Set("EntranceCode", base64.StdEncoding.EncodeToString([]byte(s.Entrance)))
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var ar struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &ar); err != nil {
		return false
	}
	return ar.Code == 200
}

// ensureMPAuth 是 /__mp/ 统一鉴权门禁：验证失败时 API 返 401，页面请求重定向至 1Panel 登录页。
func (s *Server) ensureMPAuth(w http.ResponseWriter, r *http.Request, path string) bool {
	if s.validAuthCookie(r) {
		return true
	}
	if s.localPanelLoggedIn(r) {
		s.issueAuthCookie(w)
		return true
	}

	isAPI := path == "/touch" || strings.HasPrefix(path, "/api/")
	if isAPI {
		s.denyAPI(w, "unauthorized")
		return false
	}

	target := "/"
	if s.Entrance != "" {
		target = "/" + strings.TrimPrefix(s.Entrance, "/")
	}
	http.SetCookie(w, &http.Cookie{Name: "mp_node", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, target+"?mp_return=/__mp/", http.StatusFound)
	return false
}

// denyAPI 为未经授权的 Master API 请求返回统一的 401 错误响应。
func (s *Server) denyAPI(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"code":401,"message":%q}`, msg)
}
