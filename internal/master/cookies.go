package master

import (
	"net/http"
	"strings"
)

// remoteCookiePrefix 是远端 1Panel Cookie 在浏览器侧的命名空间前缀。
const remoteCookiePrefix = "mp_r_"

// panelCookieNames 是必须与本机会话隔离的 1Panel Cookie 名。
var panelCookieNames = []string{
	"psession",
	"pcsrftoken",
	"SecurityEntrance",
	"panel_public_key",
}

// isPanelCookie 判断是否为需做命名空间隔离的面板 Cookie。
func isPanelCookie(name string) bool {
	for _, n := range panelCookieNames {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// cookieHeaderForRemote 构造发给 Agent 面板的 Cookie：mp_r_* 还原真名，丢弃本机面板会话 Cookie。
func cookieHeaderForRemote(r *http.Request) string {
	var parts []string
	for _, c := range r.Cookies() {
		if c.Name == "mp_node" || c.Name == authCookie {
			continue
		}
		if isPanelCookie(c.Name) {
			continue
		}
		if after, ok := strings.CutPrefix(c.Name, remoteCookiePrefix); ok {
			parts = append(parts, after+"="+c.Value)
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// applyRemoteRequestCookies 将请求头中的 Cookie 改写为远端命名空间视图，并校正 CSRF 头。
func applyRemoteRequestCookies(headers map[string][]string, r *http.Request) {
	delete(headers, "Cookie")
	if v := cookieHeaderForRemote(r); v != "" {
		headers["Cookie"] = []string{v}
	}
	alignRemoteCSRFHeader(headers, r)
}

// alignRemoteCSRFHeader 让 X-CSRF-Token 与远端 pcsrftoken 一致。
// 1Panel 前端从 document.cookie 读本地 pcsrftoken 填头，与 mp_r_* 会话错位会报 CSRF token invalid。
func alignRemoteCSRFHeader(headers map[string][]string, r *http.Request) {
	for k := range headers {
		if strings.EqualFold(k, "X-CSRF-Token") {
			delete(headers, k)
		}
	}
	var remoteCSRF string
	for _, c := range r.Cookies() {
		if strings.EqualFold(c.Name, remoteCookiePrefix+"pcsrftoken") {
			remoteCSRF = c.Value
			break
		}
	}
	if remoteCSRF == "" {
		// 浏览器尚无远端 CSRF：去掉本地 CSRF 头，避免与 Agent 注入的会话 Cookie 冲突。
		return
	}
	headers["X-Csrf-Token"] = []string{remoteCSRF}
}

// rewriteSetCookieToRemoteNamespace 将响应 Set-Cookie 中的面板 Cookie 改名为 mp_r_*。
// 键约定为 canonical "Set-Cookie"（来自 http.Header / HeaderFromHTTP）。
func rewriteSetCookieToRemoteNamespace(headers map[string][]string) {
	vals := headers["Set-Cookie"]
	if len(vals) == 0 {
		return
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, renameSetCookieToRemote(v))
	}
	headers["Set-Cookie"] = out
}

// renameSetCookieToRemote 改写单条 Set-Cookie：面板 Cookie 加前缀，并强制 Path=/。
func renameSetCookieToRemote(setCookie string) string {
	parts := strings.Split(setCookie, ";")
	nv := strings.TrimSpace(parts[0])
	name, value, found := strings.Cut(nv, "=")
	if !found {
		return setCookie
	}
	if isPanelCookie(name) {
		name = remoteCookiePrefix + name
	}
	parts[0] = name + "=" + value
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
