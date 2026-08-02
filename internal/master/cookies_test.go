package master

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCookieHeaderForRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "psession", Value: "local-sess"})
	r.AddCookie(&http.Cookie{Name: "mp_r_psession", Value: "remote-sess"})
	r.AddCookie(&http.Cookie{Name: "mp_node", Value: "abc"})
	r.AddCookie(&http.Cookie{Name: "other", Value: "x"})

	got := cookieHeaderForRemote(r)
	if !containsCookie(got, "psession=remote-sess") || !containsCookie(got, "other=x") {
		t.Fatalf("missing mapped cookies: %q", got)
	}
	if containsCookie(got, "psession=local-sess") || containsCookie(got, "mp_node=abc") {
		t.Fatalf("leaked local/control cookies: %q", got)
	}
}

func containsCookie(header, part string) bool {
	for _, p := range splitCookieHeader(header) {
		if p == part {
			return true
		}
	}
	return false
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

func TestRewriteSetCookieToRemoteNamespace(t *testing.T) {
	h := map[string][]string{
		"Set-Cookie": {
			"psession=abc; Path=/; HttpOnly",
			"theme=dark; Path=/",
		},
	}
	rewriteSetCookieToRemoteNamespace(h)
	vals := h["Set-Cookie"]
	if len(vals) != 2 {
		t.Fatalf("len %d", len(vals))
	}
	if vals[0] != "mp_r_psession=abc; Path=/; HttpOnly" {
		t.Fatalf("sess %q", vals[0])
	}
	if vals[1] != "theme=dark; Path=/" {
		t.Fatalf("theme %q", vals[1])
	}
}
