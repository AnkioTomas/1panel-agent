package master_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"1panel-agent/internal/agent"
	"1panel-agent/internal/config"
	"1panel-agent/internal/master"
)

func TestTunnelProxy(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Echo-Auth", r.Header.Get("1Panel-Token"))
		w.Header().Set("Set-Cookie", "sid=1; Path=/")
		fmt.Fprint(w, "panel-ok")
	}))
	defer panel.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	masterAddr := ln.Addr().String()
	_ = ln.Close()

	token := "test-token"
	srv, err := master.New(master.Options{
		Listen:     masterAddr,
		Token:      token,
		Takeover:   false,
		LocalPanel: "",
		PublicHost: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := srv.Run(); err != nil && err != http.ErrServerClosed {
			t.Logf("master exit: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".1panel-agent")
	if err := os.MkdirAll(cfgPath, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Agent{
		ID:       "agent01",
		Master:   masterAddr,
		Token:    token,
		PanelURL: panel.URL,
		PanelKey: "panel-secret",
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	go func() {
		_ = agent.Run(cfg)
	}()

	deadline := time.Now().Add(5 * time.Second)
	var listBody string
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + masterAddr + "/__mp/")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			listBody = string(b)
			if resp.StatusCode == 200 && strings.Contains(listBody, "agent01") {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(listBody, "agent01") {
		t.Fatalf("agent not listed: %s", listBody)
	}

	resp, err := http.Get("http://" + masterAddr + "/n/agent01/hello")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "panel-ok" {
		t.Fatalf("proxy status=%d body=%q", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Echo-Auth") == "" {
		t.Fatal("expected injected 1Panel-Token to reach panel")
	}
	sc := resp.Header.Get("Set-Cookie")
	if !strings.Contains(sc, "Path=/n/agent01/") {
		t.Fatalf("cookie path not rewritten: %q", sc)
	}
}
