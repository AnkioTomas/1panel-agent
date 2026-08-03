package master

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestCookieHeaderForRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "psession", Value: "remote-sess"})
	r.AddCookie(&http.Cookie{Name: "pcsrftoken", Value: "csrf"})
	r.AddCookie(&http.Cookie{Name: "mp_node", Value: "abc"})
	r.AddCookie(&http.Cookie{Name: "mp_auth", Value: "auth-token-value-32chars!!!!!!"})
	r.AddCookie(&http.Cookie{Name: "other", Value: "x"})

	got := cookieHeaderForRemote(r)
	if !containsCookie(got, "other=x") {
		t.Fatalf("missing non-panel cookie: %q", got)
	}
	if containsCookie(got, "psession=remote-sess") || containsCookie(got, "pcsrftoken=csrf") {
		t.Fatalf("leaked panel cookie: %q", got)
	}
	if containsCookie(got, "mp_node=abc") || strings.Contains(got, "mp_auth=") {
		t.Fatalf("leaked control cookie: %q", got)
	}
}

func containsCookie(header, part string) bool {
	return slices.Contains(splitCookieHeader(header), part)
}

func splitCookieHeader(h string) []string {
	var out []string
	for part := range strings.SplitSeq(h, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func TestNormalizeRemoteSetCookies(t *testing.T) {
	h := map[string][]string{
		"Set-Cookie": {
			"psession=abc; Path=/api; HttpOnly",
			"theme=dark; Path=/",
		},
	}
	normalizeRemoteSetCookies(h)
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

func TestCollectAndExpirePanelSessionCookies(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "psession", Value: "local-1"})
	r.AddCookie(&http.Cookie{Name: "pcsrftoken", Value: "csrf-1"})
	r.AddCookie(&http.Cookie{Name: "other", Value: "keep"})

	got := collectPanelSessionCookies(r)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}

	rec := httptest.NewRecorder()
	expirePanelSessionCookies(rec)
	sc := rec.Result().Header["Set-Cookie"]
	if len(sc) < 4 {
		t.Fatalf("expected expire set-cookies, got %v", sc)
	}
	joined := strings.Join(sc, "\n")
	for _, name := range []string{"psession=", "pcsrftoken=", "securityentrance=", "panel_public_key="} {
		if !strings.Contains(joined, name) {
			t.Fatalf("missing expire for %s in %q", name, joined)
		}
	}
}

func TestWritePanelSessionCookies(t *testing.T) {
	rec := httptest.NewRecorder()
	writePanelSessionCookies(rec, []*http.Cookie{
		{Name: "psession", Value: "restored"},
		{Name: "other", Value: "nope"},
	})
	sc := rec.Result().Header.Get("Set-Cookie")
	if !strings.Contains(sc, "psession=restored") {
		t.Fatalf("missing restore: %q", sc)
	}
	if strings.Contains(sc, "other=") {
		t.Fatalf("wrote non-panel cookie: %q", sc)
	}
}
