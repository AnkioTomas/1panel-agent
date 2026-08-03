package master

import (
	"net/http"
	"strings"
)

// remoteCookiePrefix 旧版远端命名空间（兼容已发出的 mp_r_*）。
const remoteCookiePrefix = "mp_r_"

// localStashPrefix 切换到子节点时，暂存本机面板会话，切回时恢复。
const localStashPrefix = "mp_l_"

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

// cookieHeaderForRemote 构造发给 Agent 的 Cookie。
//
// 切换后浏览器里的 psession/pcsrftoken 已是远端会话（SPA 直接读这些名字），
// 因此原样转发面板 Cookie；同时兼容尚未迁移的 mp_r_*。
func cookieHeaderForRemote(r *http.Request) string {
	var parts []string
	seen := map[string]bool{}
	// 1) 真名优先
	for _, c := range r.Cookies() {
		if !isPanelCookie(c.Name) {
			continue
		}
		seen[strings.ToLower(c.Name)] = true
		parts = append(parts, c.Name+"="+c.Value)
	}
	// 2) 旧 mp_r_* 兜底
	for _, c := range r.Cookies() {
		after, ok := strings.CutPrefix(c.Name, remoteCookiePrefix)
		if !ok || !isPanelCookie(after) || seen[strings.ToLower(after)] {
			continue
		}
		seen[strings.ToLower(after)] = true
		parts = append(parts, after+"="+c.Value)
	}
	// 3) 其它业务 Cookie
	for _, c := range r.Cookies() {
		switch {
		case c.Name == "mp_node" || c.Name == authCookie:
			continue
		case strings.HasPrefix(c.Name, localStashPrefix), strings.HasPrefix(c.Name, remoteCookiePrefix):
			continue
		case isPanelCookie(c.Name):
			continue
		default:
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

// applyRemoteRequestCookies 将请求头中的 Cookie 改写为远端视图，并校正 CSRF 头。
func applyRemoteRequestCookies(headers map[string][]string, r *http.Request) {
	delete(headers, "Cookie")
	if v := cookieHeaderForRemote(r); v != "" {
		headers["Cookie"] = []string{v}
	}
	alignRemoteCSRFHeader(headers, r)
}

// alignRemoteCSRFHeader 让 X-CSRF-Token 与当前远端 pcsrftoken 一致。
func alignRemoteCSRFHeader(headers map[string][]string, r *http.Request) {
	for k := range headers {
		if strings.EqualFold(k, "X-CSRF-Token") {
			delete(headers, k)
		}
	}
	csrf := ""
	for _, c := range r.Cookies() {
		if strings.EqualFold(c.Name, "pcsrftoken") {
			csrf = c.Value
			break
		}
	}
	if csrf == "" {
		for _, c := range r.Cookies() {
			if strings.EqualFold(c.Name, remoteCookiePrefix+"pcsrftoken") {
				csrf = c.Value
				break
			}
		}
	}
	if csrf == "" {
		return
	}
	headers["X-Csrf-Token"] = []string{csrf}
}

// rewriteSetCookieForRemote 远端模式下面板 Cookie 保持真名（供 SPA 直接读取）。
// 不再改成 mp_r_*：否则前端仍读本机 psession，第一次切节点必掉登录页。
func rewriteSetCookieForRemote(headers map[string][]string) {
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

// forceCookiePathRoot 强制 Path=/，便于整站会话。
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

// stashLocalPanelCookies 把当前本机面板会话写入 mp_l_*，避免切子节点时被远端 psession 覆盖后无法回切。
func stashLocalPanelCookies(w http.ResponseWriter, r *http.Request) {
	for _, c := range r.Cookies() {
		if !isPanelCookie(c.Name) {
			continue
		}
		http.SetCookie(w, &http.Cookie{
			Name:     localStashPrefix + c.Name,
			Value:    c.Value,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// restoreLocalPanelCookies 从 mp_l_* 恢复本机会话，并清掉暂存/旧 mp_r_*。
func restoreLocalPanelCookies(w http.ResponseWriter, r *http.Request) {
	for _, c := range r.Cookies() {
		if after, ok := strings.CutPrefix(c.Name, localStashPrefix); ok && isPanelCookie(after) {
			http.SetCookie(w, &http.Cookie{
				Name:     after,
				Value:    c.Value,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.SetCookie(w, &http.Cookie{
				Name:     c.Name,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				SameSite: http.SameSiteLaxMode,
			})
		}
		if after, ok := strings.CutPrefix(c.Name, remoteCookiePrefix); ok && isPanelCookie(after) {
			http.SetCookie(w, &http.Cookie{
				Name:     c.Name,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				SameSite: http.SameSiteLaxMode,
			})
		}
	}
}

// writePanelCookies 把面板会话 Cookie 写到浏览器（真名，供 SPA 使用）。
func writePanelCookies(w http.ResponseWriter, cookies []*http.Cookie) {
	for _, c := range cookies {
		if !isPanelCookie(c.Name) {
			continue
		}
		sc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     "/",
			HttpOnly: c.HttpOnly,
			SameSite: http.SameSiteLaxMode,
			Secure:   c.Secure,
		}
		if c.MaxAge > 0 {
			sc.MaxAge = c.MaxAge
		}
		http.SetCookie(w, sc)
	}
}

// expirePanelCookies 清除浏览器中的面板会话 Cookie。
func expirePanelCookies(w http.ResponseWriter) {
	for _, name := range panelCookieNames {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
		})
	}
}
