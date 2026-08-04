package agent

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

// EnsureLocalPanelHardening 强制 Agent 本机面板：禁用 SSL + 只监听 127.0.0.1。
// 安装后与每次 agent 进程启动都检查；已合规则静默跳过。
func EnsureLocalPanelHardening(cfg *config.Agent) error {
	if cfg == nil {
		return fmt.Errorf("nil agent config")
	}
	if err := AutofillPanel(cfg); err != nil {
		return err
	}
	port, err := portFromPanelURL(cfg.PanelURL)
	if err != nil {
		return err
	}
	needSSLOff := panel.PanelSSLReady()
	needBind := listensExternally(port)
	if !needSSLOff && !needBind {
		return nil
	}
	log.Printf("local panel hardening: ssl_off=%v bind_localhost=%v", needSSLOff, needBind)

	pass, err := cfg.PanelPasswordPlain()
	if err != nil || pass == "" {
		return fmt.Errorf("panel password required for hardening")
	}
	if cfg.PanelUser == "" {
		return fmt.Errorf("panel user unknown")
	}

	client := panel.NewInsecureClient(2 * time.Minute)
	base := strings.TrimRight(cfg.PanelURL, "/")

	if needSSLOff {
		res, err := panel.Login(base, cfg.PanelEntrance, cfg.PanelUser, pass)
		if err != nil {
			return fmt.Errorf("login for ssl disable: %w", err)
		}
		if err := panel.UpdateSSL(client, base, cfg.PanelEntrance, res.Cookies, false, ""); err != nil {
			return fmt.Errorf("disable ssl: %w", err)
		}
		if err := waitPanelSSLReady(false, 60*time.Second); err != nil {
			return err
		}
		// SSL 关闭后面板重启，scheme 变 http
		cfg.PanelURL = panel.LocalPanelURL(port)
		base = cfg.PanelURL
		_ = config.Save(cfg)
		needBind = listensExternally(port) // 重启后可能仍对外
	}

	if needBind {
		res, err := panel.Login(base, cfg.PanelEntrance, cfg.PanelUser, pass)
		if err != nil {
			return fmt.Errorf("login for bind: %w", err)
		}
		if err := panel.UpdateBindInfo(client, base, cfg.PanelEntrance, res.Cookies, "127.0.0.1"); err != nil {
			return fmt.Errorf("bind 127.0.0.1: %w", err)
		}
		if err := waitListenLocalhost(port, 60*time.Second); err != nil {
			return err
		}
	}

	if err := AutofillPanel(cfg); err != nil {
		return err
	}
	cfg.PanelURL = panel.LocalPanelURL(port)
	_ = config.Save(cfg)
	log.Printf("local panel hardened: url=%s", cfg.PanelURL)
	return nil
}

func waitPanelSSLReady(want bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if panel.PanelSSLReady() == want {
			time.Sleep(2 * time.Second)
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout waiting panel ssl ready=%v", want)
}

func waitListenLocalhost(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !listensExternally(port) {
			time.Sleep(time.Second)
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout waiting panel bind 127.0.0.1:%d", port)
}

func portFromPanelURL(raw string) (int, error) {
	host := raw
	if i := strings.LastIndex(raw, "://"); i >= 0 {
		host = raw[i+3:]
	}
	host = strings.TrimSuffix(strings.SplitN(host, "/", 2)[0], "/")
	_, portStr, err := net.SplitHostPort(host)
	if err != nil {
		return 0, fmt.Errorf("panel_url missing port: %q", raw)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return 0, fmt.Errorf("panel_url invalid port: %q", raw)
	}
	return p, nil
}

// listensExternally 判断面板端口是否仍在 0.0.0.0/* 上监听（允许外部访问）。
func listensExternally(port int) bool {
	if port <= 0 {
		return false
	}
	out, err := exec.Command("ss", "-lntp").CombinedOutput()
	if err != nil {
		// ss 不可用时保守：不强制改绑定，避免误伤
		return false
	}
	needle := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		// 0.0.0.0:port / *:port / [::]:port 视为对外
		if strings.Contains(line, "0.0.0.0:"+strconv.Itoa(port)) ||
			strings.Contains(line, "*:"+strconv.Itoa(port)) ||
			strings.Contains(line, "[::]:"+strconv.Itoa(port)) {
			return true
		}
	}
	return false
}
