package master

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
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
	panel.ApplyEntrance(req.Header, s.Entrance)
	resp, err := panel.NewInsecureClient(5 * time.Second).Do(req)
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

	s.redirectToMasterLogin(w, r, "/__mp/")
	return false
}

// clearNodeCookie 清除 mp_node，确保后续请求落在主节点。
func clearNodeCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
	})
}

// masterLoginPath 返回主节点登录入口路径（含安全入口）。
func (s *Server) masterLoginPath() string {
	if s.Entrance != "" {
		return "/" + strings.TrimPrefix(s.Entrance, "/")
	}
	return "/"
}

// redirectToMasterLogin 清掉 mp_node 后跳到主节点登录页（可选带 mp_return）。
func (s *Server) redirectToMasterLogin(w http.ResponseWriter, r *http.Request, mpReturn string) {
	clearNodeCookie(w)
	target := s.masterLoginPath()
	if mpReturn != "" {
		target = target + "?mp_return=" + mpReturn
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// isLoginPagePath 判断是否为面板登录页路径（排除 /api/）。
func isLoginPagePath(path string) bool {
	p := strings.ToLower(path)
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if strings.HasPrefix(p, "/api/") {
		return false
	}
	return p == "/login" || strings.HasSuffix(p, "/login")
}

// locationIsLoginRedirect 判断 Location 是否指向登录页。
func locationIsLoginRedirect(headers map[string][]string) bool {
	for k, vals := range headers {
		if !strings.EqualFold(k, "Location") || len(vals) == 0 {
			continue
		}
		loc := vals[0]
		// 相对或绝对 URL：只看 path 段
		if i := strings.Index(loc, "://"); i >= 0 {
			rest := loc[i+3:]
			if j := strings.IndexByte(rest, '/'); j >= 0 {
				loc = rest[j:]
			} else {
				loc = "/"
			}
		}
		if isLoginPagePath(loc) {
			return true
		}
	}
	return false
}

// denyAPI 为未经授权的 Master API 请求返回统一的 401 错误响应。
func (s *Server) denyAPI(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"code":401,"message":%q}`, msg)
}
