package master

import (
	"net/http"
	"strings"
)

// panelSessionCookieNames 是 1Panel 面板会话相关 Cookie，切节点时由 Master/Agent 各自持有，
// 不得作为控制面 Cookie 原样在隧道两侧混用。
var panelSessionCookieNames = map[string]struct{}{
	"psession":         {},
	"pcsrftoken":       {},
	"securityentrance": {},
	"panel_public_key": {},
}

func isPanelSessionCookie(name string) bool {
	_, ok := panelSessionCookieNames[strings.ToLower(name)]
	return ok
}

// collectPanelSessionCookies 从请求中抽出本机面板会话 Cookie（用于切走时暂存）。
func collectPanelSessionCookies(r *http.Request) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range r.Cookies() {
		if !isPanelSessionCookie(c.Name) || c.Value == "" {
			continue
		}
		out = append(out, &http.Cookie{
			Name:  c.Name,
			Value: c.Value,
			Path:  "/",
		})
	}
	return out
}

// expirePanelSessionCookies 让浏览器丢掉面板会话 Cookie。
func expirePanelSessionCookies(w http.ResponseWriter) {
	for name := range panelSessionCookieNames {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// writePanelSessionCookies 把暂存的本机面板会话写回浏览器。
func writePanelSessionCookies(w http.ResponseWriter, cookies []*http.Cookie) {
	for _, c := range cookies {
		if c == nil || !isPanelSessionCookie(c.Name) || c.Value == "" {
			continue
		}
		http.SetCookie(w, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// cookieHeaderForRemote 构造发给 Agent 的 Cookie。
// 过滤 Master 控制 Cookie 与面板会话 Cookie：远端会话由 Agent 自己注入。
func cookieHeaderForRemote(r *http.Request) string {
	var parts []string
	for _, c := range r.Cookies() {
		if c.Name == "mp_node" || c.Name == authCookie || isPanelSessionCookie(c.Name) {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// applyRemoteRequestCookies 写入远端 Cookie 视图。
func applyRemoteRequestCookies(headers map[string][]string, r *http.Request) {
	delete(headers, "Cookie")
	if v := cookieHeaderForRemote(r); v != "" {
		headers["Cookie"] = []string{v}
	}
}

// normalizeRemoteSetCookies 强制面板 Set-Cookie 的 Path=/，并透传给浏览器
// （Agent 自动登录会覆盖浏览器里的面板 Cookie，这是预期行为）。
func normalizeRemoteSetCookies(headers map[string][]string) {
	vals := headers["Set-Cookie"]
	if len(vals) == 0 {
		return
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, forceCookiePathRoot(v))
	}
	headers["Set-Cookie"] = out
}

func forceCookiePathRoot(setCookie string) string {
	parts := strings.Split(setCookie, ";")
	for i := 1; i < len(parts); i++ {
		trim := strings.TrimSpace(parts[i])
		if len(trim) >= 5 && strings.EqualFold(trim[:5], "Path=") {
			parts[i] = " Path=/"
		} else {
			parts[i] = " " + trim
		}
	}
	return strings.Join(parts, ";")
}
