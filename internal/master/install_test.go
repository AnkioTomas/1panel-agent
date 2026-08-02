package master

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallCommand(t *testing.T) {
	s := &Server{Token: "secret", PublicHost: "10.211.55.14", Listen: ":52045"}
	r := httptest.NewRequest("GET", "http://10.211.55.14:52045/__mp/", nil)
	cmd := s.InstallCommand(r)
	want := `curl -fsSL "http://10.211.55.14:52045/agent.sh?token=secret" | sudo bash`
	if cmd != want {
		t.Fatalf("got %q want %q", cmd, want)
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
		"/agent.bin?token=",
		"agent register",
		"1pm-agent.service",
		"systemctl restart",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
}
