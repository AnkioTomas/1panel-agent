package master

import (
	"net/http"
	"strings"
)

// localStashPrefix 切换到子节点时暂存本机面板会话，切回时恢复。
const localStashPrefix = "mp_l_"

// panelCookieNames 本机/远端切换时需要互换的 1Panel 会话 Cookie。
var panelCookieNames = []string{
	"psession",
	"pcsrftoken",
	"SecurityEntrance",
	"panel_public_key",
}

func isPanelCookie(name string) bool {
	for _, n := range panelCookieNames {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// cookieHeaderForRemote 构造发给 Agent 的 Cookie。
// 切换后浏览器里的 psession/pcsrftoken 就是远端会话，原样转发；mp_l_* 只是本机暂存，不外发。
func cookieHeaderForRemote(r *http.Request) string {
	var parts []string
	for _, c := range r.Cookies() {
		switch {
		case c.Name == "mp_node" || c.Name == authCookie:
			continue
		case strings.HasPrefix(c.Name, localStashPrefix):
			continue
		default:
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

// applyRemoteRequestCookies 写入远端 Cookie 视图，并让 CSRF 头与 pcsrftoken 一致。
func applyRemoteRequestCookies(headers map[string][]string, r *http.Request) {
	delete(headers, "Cookie")
	if v := cookieHeaderForRemote(r); v != "" {
		headers["Cookie"] = []string{v}
	}
	alignRemoteCSRFHeader(headers, r)
}

// alignRemoteCSRFHeader 用 Cookie 中的 pcsrftoken 覆盖 X-CSRF-Token（1Panel 双提交）。
func alignRemoteCSRFHeader(headers map[string][]string, r *http.Request) {
	for k := range headers {
		if strings.EqualFold(k, "X-CSRF-Token") {
			delete(headers, k)
		}
	}
	var csrf string
	for _, c := range r.Cookies() {
		if strings.EqualFold(c.Name, "pcsrftoken") {
			csrf = c.Value
			break
		}
	}
	if csrf == "" {
		return
	}
	headers["X-Csrf-Token"] = []string{csrf}
}

// normalizeRemoteSetCookies 强制面板 Set-Cookie 的 Path=/。
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

// stashLocalPanelCookies 把本机面板会话写入 mp_l_*。
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

// restoreLocalPanelCookies 从 mp_l_* 恢复本机会话并删除暂存。
func restoreLocalPanelCookies(w http.ResponseWriter, r *http.Request) {
	for _, c := range r.Cookies() {
		after, ok := strings.CutPrefix(c.Name, localStashPrefix)
		if !ok || !isPanelCookie(after) {
			continue
		}
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
}

// writePanelCookies 把面板会话 Cookie 写到浏览器（真名）。
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
