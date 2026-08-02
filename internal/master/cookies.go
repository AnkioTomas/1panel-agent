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

func remoteCookieName(name string) string {
	return remoteCookiePrefix + name
}

func localCookieName(remoteName string) (string, bool) {
	if !strings.HasPrefix(remoteName, remoteCookiePrefix) {
		return "", false
	}
	return strings.TrimPrefix(remoteName, remoteCookiePrefix), true
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
			// local session — do not send to remote
			continue
		}
		if real, ok := localCookieName(c.Name); ok {
			parts = append(parts, real+"="+c.Value)
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
		// case-insensitive pick
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
		out = append(out, renameSetCookie(v, true))
	}
	headers["Set-Cookie"] = out
}

func renameSetCookie(setCookie string, toRemote bool) string {
	parts := strings.Split(setCookie, ";")
	if len(parts) == 0 {
		return setCookie
	}
	nv := strings.TrimSpace(parts[0])
	name, value, found := strings.Cut(nv, "=")
	if !found {
		return setCookie
	}
	if toRemote {
		if isPanelCookie(name) {
			name = remoteCookieName(name)
		}
	} else {
		if real, ok := localCookieName(name); ok {
			name = real
		}
	}
	parts[0] = name + "=" + value
	// normalize Path to /
	for i := 1; i < len(parts); i++ {
		trim := strings.TrimSpace(parts[i])
		if len(trim) >= 5 && strings.EqualFold(trim[:5], "Path=") {
			parts[i] = " Path=/"
		} else if i > 0 {
			parts[i] = " " + trim
		}
	}
	return strings.Join(parts, ";")
}

func setRemotePanelCookie(w http.ResponseWriter, name, value string, httpOnly bool) {
	if isPanelCookie(name) {
		name = remoteCookieName(name)
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
