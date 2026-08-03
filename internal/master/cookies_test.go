package master

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"1panel-agent/internal/protocol"
)

func TestCookieHeaderForRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "psession", Value: "remote-sess"})
	r.AddCookie(&http.Cookie{Name: "mp_r_psession", Value: "legacy-remote"})
	r.AddCookie(&http.Cookie{Name: "mp_l_psession", Value: "stashed-local"})
	r.AddCookie(&http.Cookie{Name: "mp_node", Value: "abc"})
	r.AddCookie(&http.Cookie{Name: "other", Value: "x"})

	got := cookieHeaderForRemote(r)
	// psession 优先（切换后真名已是远端）；mp_r_* 不应再覆盖；mp_l_* 不外泄
	if !containsCookie(got, "psession=remote-sess") || !containsCookie(got, "other=x") {
		t.Fatalf("missing mapped cookies: %q", got)
	}
	if containsCookie(got, "psession=legacy-remote") || containsCookie(got, "stashed-local") || containsCookie(got, "mp_node=abc") {
		t.Fatalf("leaked stash/control cookies: %q", got)
	}
}

func containsCookie(header, part string) bool {
	return slices.Contains(splitCookieHeader(header), part)
}

func splitCookieHeader(h string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(h); i++ {
		if i == len(h) || h[i] == ';' {
			part := trimSpace(h[start:i])
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func TestRewriteSetCookieForRemoteKeepsRealNames(t *testing.T) {
	h := map[string][]string{
		"Set-Cookie": {
			"psession=abc; Path=/api; HttpOnly",
			"theme=dark; Path=/",
		},
	}
	rewriteSetCookieForRemote(h)
	vals := h["Set-Cookie"]
	if len(vals) != 2 {
		t.Fatalf("len %d", len(vals))
	}
	if vals[0] != "psession=abc; Path=/; HttpOnly" {
		t.Fatalf("sess %q", vals[0])
	}
	if vals[1] != "theme=dark; Path=/" {
		t.Fatalf("theme %q", vals[1])
	}
}

func TestAlignRemoteCSRFHeaderUsesRealName(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: "pcsrftoken", Value: "remote-csrf"})
	r.Header.Set("X-CSRF-Token", "stale")

	headers := protocol.HeaderFromHTTP(r.Header)
	applyRemoteRequestCookies(headers, r)

	got := ""
	for k, vals := range headers {
		if strings.EqualFold(k, "X-CSRF-Token") && len(vals) > 0 {
			got = vals[0]
		}
	}
	if got != "remote-csrf" {
		t.Fatalf("csrf header=%q want remote-csrf; headers=%v", got, headers)
	}
}

func TestAlignRemoteCSRFHeaderLegacyMPR(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: "mp_r_pcsrftoken", Value: "legacy-csrf"})
	r.Header.Set("X-CSRF-Token", "stale")

	headers := protocol.HeaderFromHTTP(r.Header)
	applyRemoteRequestCookies(headers, r)

	got := ""
	for k, vals := range headers {
		if strings.EqualFold(k, "X-CSRF-Token") && len(vals) > 0 {
			got = vals[0]
		}
	}
	if got != "legacy-csrf" {
		t.Fatalf("csrf header=%q want legacy-csrf", got)
	}
}

func TestAlignRemoteCSRFHeaderStripsWhenMissing(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-CSRF-Token", "local-csrf")

	headers := protocol.HeaderFromHTTP(r.Header)
	applyRemoteRequestCookies(headers, r)
	for k := range headers {
		if strings.EqualFold(k, "X-CSRF-Token") {
			t.Fatalf("csrf header must be stripped, got %v", headers[k])
		}
	}
}
