package master

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"1panel-agent/internal/config"
)

func TestInstallCommand(t *testing.T) {
	s := &Server{Token: "secret", PublicHost: "10.211.55.14", Listen: ":52045"}
	r := httptest.NewRequest("GET", "http://10.211.55.14:52045/__mp/", nil)
	cmd := s.InstallCommand(r)
	if !strings.Contains(cmd, "http://10.211.55.14:52045/agent.sh?timestamp=") {
		t.Fatalf("missing signed url: %q", cmd)
	}
	if !strings.Contains(cmd, "&sign=") {
		t.Fatalf("missing sign: %q", cmd)
	}
	if strings.Contains(cmd, "token=secret") {
		t.Fatalf("raw token must not appear in install curl: %q", cmd)
	}
}

func TestAuthorizeAgentDownload(t *testing.T) {
	s := &Server{Token: "secret"}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign("secret", ts)
	ok := httptest.NewRequest(http.MethodGet, "/agent.sh?timestamp="+ts+"&sign="+sign, nil)
	w := httptest.NewRecorder()
	if !s.authorizeAgentDownload(w, ok) {
		t.Fatal("expected authorize")
	}
	bad := httptest.NewRequest(http.MethodGet, "/agent.sh?token=secret", nil)
	w = httptest.NewRecorder()
	if s.authorizeAgentDownload(w, bad) {
		t.Fatal("raw token must be rejected")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestAgentInstallScript(t *testing.T) {
	var buf bytes.Buffer
	err := agentInstallTmpl.Execute(&buf, struct {
		Base, Master, Token, GOOS, GOARCH string
	}{
		Base:   "http://10.211.55.14:52045",
		Master: "10.211.55.14:52045",
		Token:  "secret",
		GOOS:   "linux",
		GOARCH: "arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, needle := range []string{
		"#!/bin/bash",
		"sign_query",
		"/agent.bin?",
		"agent install",
		"ask_panel_password",
		"agent setpwd",
		"agent run",
		"1pm-agent.service",
		"安装完成",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
	if strings.Contains(out, "agent register") {
		t.Fatal("systemd must not call agent register")
	}
	if strings.Contains(out, "Enter to skip") {
		t.Fatal("password must be required, not skippable")
	}
}
