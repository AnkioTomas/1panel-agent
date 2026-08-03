package master_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"1panel-agent/internal/agent"
	"1panel-agent/internal/config"
	"1panel-agent/internal/master"
)

func TestTunnelProxy(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/dashboard/base/os" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"code":200}`)
			return
		}
		if r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Echo-Auth", r.Header.Get("1Panel-Token"))
		w.Header().Set("Set-Cookie", "psession=1; Path=/")
		fmt.Fprint(w, "panel-ok")
	}))
	defer panel.Close()

	home := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	masterPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	u, _ := url.Parse(panel.URL)
	panelPort, _ := strconv.Atoi(u.Port())

	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	mock1panel := filepath.Join(binDir, "1panel")
	mockScript := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-l" ] && [ "$3" = "update" ] && [ "$4" = "port" ]; then
    echo "Update successful!"
    exit 0
fi
echo 'Panel address: http://127.0.0.1:%d/'
echo 'User: testuser'
`, masterPort)
	_ = os.WriteFile(mock1panel, []byte(mockScript), 0o755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	// Pre-create master config so EnsureTakeover won't fail when 1Panel CLI is missing in tests
	_ = config.SaveMaster(&config.Master{
		OriginalPort: masterPort,
		InternalPort: panelPort,
	})

	srv, err := master.New()
	if err != nil {
		t.Fatal(err)
	}
	srv.LocalPanel = panel.URL
	masterAddr := srv.Listen
	if strings.HasPrefix(masterAddr, ":") {
		masterAddr = "127.0.0.1" + masterAddr
	}
	token := srv.Token
	go func() {
		if err := srv.Run(); err != nil && err != http.ErrServerClosed {
			t.Logf("master exit: %v", err)
		}
	}()
	cfgPath := filepath.Join(home, ".1panel-agent")
	if err := os.MkdirAll(cfgPath, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Agent{
		ID:       "agent01",
		Master:   masterAddr,
		Token:    token,
		PanelURL: panel.URL,
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
		req, _ := http.NewRequest(http.MethodGet, "http://"+masterAddr+"/__mp/api/agents", nil)
		req.AddCookie(&http.Cookie{Name: "psession", Value: "1"})
		resp, err := http.DefaultClient.Do(req)
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

	// Production path: mp_node cookie + root-path tunnel.
	req, err := http.NewRequest(http.MethodGet, "http://"+masterAddr+"/hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "mp_node", Value: "agent01"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "panel-ok" {
		t.Fatalf("proxy status=%d body=%q", resp.StatusCode, body)
	}
	sc := resp.Header.Get("Set-Cookie")
	if !strings.Contains(sc, "psession=") {
		t.Fatalf("panel cookie missing: %q", sc)
	}
}
