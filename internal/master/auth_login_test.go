package master

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsLoginPagePath(t *testing.T) {
	cases := map[string]bool{
		"/login":                  true,
		"/Login":                  true,
		"/foo/login":              true,
		"/login?x=1":              true,
		"/api/v2/core/auth/login": false,
		"/api/login":              false,
		"/settings":               false,
		"/":                       false,
	}
	for p, want := range cases {
		if got := isLoginPagePath(p); got != want {
			t.Fatalf("%s: got %v want %v", p, got, want)
		}
	}
}

func TestLocationIsLoginRedirect(t *testing.T) {
	if !locationIsLoginRedirect(map[string][]string{"Location": {"/login"}}) {
		t.Fatal("relative")
	}
	if !locationIsLoginRedirect(map[string][]string{"Location": {"http://x/login"}}) {
		t.Fatal("absolute")
	}
	if locationIsLoginRedirect(map[string][]string{"Location": {"/api/v2/core/auth/login"}}) {
		t.Fatal("api login must not match")
	}
}

func TestRedirectToMasterLoginClearsNode(t *testing.T) {
	s := &Server{Entrance: "tomas"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.redirectToMasterLogin(rec, req, "/__mp/")
	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/tomas?mp_return=/__mp/" {
		t.Fatalf("loc %q", loc)
	}
	sc := strings.Join(rec.Header()["Set-Cookie"], ";")
	if !strings.Contains(sc, "mp_node=") {
		t.Fatalf("mp_node not cleared: %v", rec.Header()["Set-Cookie"])
	}
}

func TestRedirectToMasterLoginRestoresStash(t *testing.T) {
	s := &Server{Entrance: "tomas"}
	s.stashLocalSession([]*http.Cookie{{Name: "psession", Value: "local-sess", Path: "/"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: "mp_node", Value: "agent01"})
	req.AddCookie(&http.Cookie{Name: "psession", Value: "remote-sess"})
	s.redirectToMasterLogin(rec, req, "")

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("want / got %q (should restore, not force login)", loc)
	}
	sc := strings.Join(rec.Header()["Set-Cookie"], ";")
	if !strings.Contains(sc, "psession=local-sess") {
		t.Fatalf("local session not restored: %v", rec.Header()["Set-Cookie"])
	}
	if !strings.Contains(sc, "mp_node=") {
		t.Fatalf("mp_node not cleared: %v", rec.Header()["Set-Cookie"])
	}
}
