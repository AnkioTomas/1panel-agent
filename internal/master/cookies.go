package master

import (
	"net/http"
	"strings"
)

// cookieHeaderForRemote 构造发给 Agent 的 Cookie。
// 过滤掉 Master 内部使用的 mp_node 和 authCookie。
func cookieHeaderForRemote(r *http.Request) string {
	var parts []string
	for _, c := range r.Cookies() {
		if c.Name == "mp_node" || c.Name == authCookie {
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
