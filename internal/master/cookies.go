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
		if strings.HasPrefix(c.Name, remoteCookiePrefix) {
			parts = append(parts, strings.TrimPrefix(c.Name, remoteCookiePrefix)+"="+c.Value)
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// applyRemoteRequestCookies 将请求头中的 Cookie 改写为远端命名空间视图。
func applyRemoteRequestCookies(headers map[string][]string, r *http.Request) {
	delete(headers, "Cookie")
	if v := cookieHeaderForRemote(r); v != "" {
		headers["Cookie"] = []string{v}
	}
}

// rewriteSetCookieToRemoteNamespace 将响应 Set-Cookie 中的面板 Cookie 改名为 mp_r_*。
func rewriteSetCookieToRemoteNamespace(headers map[string][]string) {
	vals, ok := headers["Set-Cookie"]
	if !ok {
		for k, v := range headers {
			if strings.EqualFold(k, "Set-Cookie") {
				vals = v
				delete(headers, k)
				ok = true
				break
			}
		}
	} else {
		delete(headers, "Set-Cookie")
	}
	if !ok || len(vals) == 0 {
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
	if len(parts) == 0 {
		return setCookie
	}
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

// setRemotePanelCookie 向浏览器写入远端命名空间下的面板 Cookie。
func setRemotePanelCookie(w http.ResponseWriter, name, value string, httpOnly bool) {
	if isPanelCookie(name) {
		name = remoteCookiePrefix + name
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: httpOnly,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
}
