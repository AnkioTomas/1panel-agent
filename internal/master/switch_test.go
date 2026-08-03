package master

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xtaci/smux"
)

func TestStashAndRestoreLocalSession(t *testing.T) {
	s := &Server{reg: NewRegistry()}

	req := httptest.NewRequest(http.MethodGet, "/__mp/go/agent01", nil)
	req.AddCookie(&http.Cookie{Name: "psession", Value: "local-sess"})
	req.AddCookie(&http.Cookie{Name: "pcsrftoken", Value: "local-csrf"})

	rec := httptest.NewRecorder()
	s.handleSwitch(rec, req, "missing")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("offline status=%d", rec.Code)
	}

	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	go func() {
		srv, err := smux.Server(c2, nil)
		if err != nil {
			return
		}
		defer srv.Close()
		for {
			st, err := srv.AcceptStream()
			if err != nil {
				return
			}
			_ = st.Close()
		}
	}()
	mux, err := smux.Client(c1, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Close() })
	s.reg.Put(&Session{Info: AgentInfo{ID: "agent01"}, Mux: mux})

	rec = httptest.NewRecorder()
	s.handleSwitch(rec, req, "agent01")
	if rec.Code != http.StatusFound {
		t.Fatalf("switch status=%d", rec.Code)
	}
	setCookies := rec.Result().Header["Set-Cookie"]
	hasNode, hasExpire := false, false
	for _, sc := range setCookies {
		if strings.HasPrefix(sc, "mp_node=agent01") {
			hasNode = true
		}
		if strings.HasPrefix(sc, "psession=") && (strings.Contains(sc, "Max-Age=-1") || strings.Contains(sc, "Max-Age=0")) {
			hasExpire = true
		}
	}
	if !hasNode {
		t.Fatalf("mp_node missing: %v", setCookies)
	}
	if !hasExpire {
		t.Fatalf("psession not expired: %v", setCookies)
	}

	s.localSessMu.Lock()
	n := len(s.localSessCookies)
	s.localSessMu.Unlock()
	if n != 2 {
		t.Fatalf("stash len=%d", n)
	}

	rec = httptest.NewRecorder()
	s.handleLocal(rec, httptest.NewRequest(http.MethodGet, "/__mp/local", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("local status=%d", rec.Code)
	}
	restored := false
	clearedNode := false
	for _, sc := range rec.Result().Header["Set-Cookie"] {
		if strings.HasPrefix(sc, "psession=local-sess") {
			restored = true
		}
		if strings.HasPrefix(sc, "mp_node=") && (strings.Contains(sc, "Max-Age=-1") || strings.Contains(sc, "Max-Age=0")) {
			clearedNode = true
		}
	}
	if !restored {
		t.Fatalf("local session not restored: %v", rec.Result().Header["Set-Cookie"])
	}
	if !clearedNode {
		t.Fatalf("mp_node not cleared: %v", rec.Result().Header["Set-Cookie"])
	}

	s.localSessMu.Lock()
	left := len(s.localSessCookies)
	s.localSessMu.Unlock()
	if left != 0 {
		t.Fatalf("stash not consumed: %d", left)
	}
}
