package master

import (
	"net/http"
	"strings"
)

const remoteCookiePrefix = "mp_r_"

// panelCookieNames are 1Panel session cookies that must not collide between local and remote.
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

// cookieHeaderForRemote builds Cookie header for the agent panel:
// map mp_r_* -> real names; drop local panel session cookies.
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

// applyRemoteRequestCookies rewrites headers so the tunnel sees remote session cookies.
func applyRemoteRequestCookies(headers map[string][]string, r *http.Request) {
	delete(headers, "Cookie")
	if v := cookieHeaderForRemote(r); v != "" {
		headers["Cookie"] = []string{v}
	}
}

// rewriteSetCookieToRemoteNamespace renames panel Set-Cookie names to mp_r_*.
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
