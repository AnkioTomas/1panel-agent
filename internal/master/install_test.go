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
	if !strings.Contains(cmd, "http://10.211.55.14:52045/agent.sh?") {
		t.Fatalf("missing signed url: %q", cmd)
	}
	if !strings.Contains(cmd, "timestamp=") {
		t.Fatalf("missing timestamp: %q", cmd)
	}
	if !strings.Contains(cmd, "sign=") {
		t.Fatalf("missing sign: %q", cmd)
	}
	if strings.Contains(cmd, "token=secret") {
		t.Fatalf("raw token must not appear in install curl: %q", cmd)
	}

	r2 := httptest.NewRequest("GET", "http://10.211.55.14:52045/__mp/api/install-command?name=web1&group=prod", nil)
	cmd2 := s.InstallCommand(r2)
	if !strings.Contains(cmd2, "name=web1") || !strings.Contains(cmd2, "group=prod") {
		t.Fatalf("missing name/group query: %q", cmd2)
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
		Base, Master, Token, Name, Group               string
		Repo, GitHubAPI, GitHubDL, InstallCDN, Version string
		MasterTLS                                      bool
	}{
		Base:       "https://10.211.55.14:52045",
		Master:     "10.211.55.14:52045",
		Token:      "secret",
		MasterTLS:  true,
		Name:       "web-1",
		Group:      "prod",
		Repo:       "AnkioTomas/1panel-agent",
		GitHubAPI:  "https://api.github.com",
		GitHubDL:   "https://github.com",
		InstallCDN: "auto",
		Version:    "v0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, needle := range []string{
		"#!/bin/bash",
		"agent install",
		`--name "$NODE_NAME"`,
		`--group "$NODE_GROUP"`,
		"--master-tls",
		"MASTER_TLS=1",
		"INSTALL_CDN=",
		"gh-proxy.com",
		"1pm_linux_",
		"releases/download",
		"ask_panel_password",
		"save_panel_password",
		"agent setpwd",
		"agent run",
		"1pm-agent.service",
		"1pm-master.service",
		"不能同时作为 agent",
		"安装完成",
		"from CDN",
		`NODE_NAME="web-1"`,
		`NODE_GROUP="prod"`,
		`VERSION="v0.1.0"`,
		"Environment=INSTALL_CDN=",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
	if strings.Contains(out, "/agent.bin?") {
		t.Fatal("agent.sh must download from CDN, not /agent.bin")
	}
	if strings.Contains(out, "agent register") {
		t.Fatal("systemd must not call agent register")
	}
	if strings.Contains(out, "Enter to skip") {
		t.Fatal("password must be required, not skippable")
	}
}
