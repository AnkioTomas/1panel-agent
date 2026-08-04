package agent

import (
	"net/http"
	"testing"

	"1panel-agent/internal/panel"
)

func TestApplyAgentSession(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "psession=stale; theme=dark; pcsrftoken=old")
	applyAgentSession(req, []*http.Cookie{
		{Name: "psession", Value: "fresh"},
		{Name: "pcsrftoken", Value: "csrf"},
	})
	got := req.Header.Get("Cookie")
	if panel.CookieValue(got, "psession") != "fresh" {
		t.Fatalf("psession=%q", got)
	}
	if panel.CookieValue(got, "pcsrftoken") != "csrf" {
		t.Fatalf("csrf=%q", got)
	}
	if panel.CookieValue(got, "theme") != "dark" {
		t.Fatalf("theme lost: %q", got)
	}
	panel.AlignCSRF(req.Header)
	if req.Header.Get("X-CSRF-Token") != "csrf" {
		t.Fatalf("csrf header=%q", req.Header.Get("X-CSRF-Token"))
	}
}

func TestPanelUnauthenticated(t *testing.T) {
	if !panelUnauthenticated(http.StatusUnauthorized, nil) {
		t.Fatal("401 status")
	}
	if !panelUnauthenticated(http.StatusOK, []byte(`{"code":401,"message":"ssomething"}`)) {
		t.Fatal("json 401")
	}
	if panelUnauthenticated(http.StatusOK, []byte(`{"code":200}`)) {
		t.Fatal("false positive")
	}
	if panelUnauthenticated(http.StatusOK, []byte(`<html>`)) {
		t.Fatal("html false positive")
	}
}
