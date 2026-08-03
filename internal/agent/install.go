package agent

import (
	"fmt"
	"strings"

	"1panel-agent/internal/config"
	"1panel-agent/internal/role"
)

// ParseInstallTarget 解析 "host:port/token" 格式（兼容旧写法）。
func ParseInstallTarget(s string) (master, token string, err error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "ws://")
	s = strings.TrimPrefix(s, "wss://")

	idx := strings.LastIndex(s, "/")
	if idx <= 0 || idx == len(s)-1 {
		return "", "", fmt.Errorf("invalid install target %q; want host:port/token", s)
	}
	master = s[:idx]
	token = s[idx+1:]
	if master == "" || token == "" {
		return "", "", fmt.Errorf("invalid install target %q; want host:port/token", s)
	}
	if !strings.Contains(master, ":") {
		return "", "", fmt.Errorf("master must include port: %q", master)
	}
	return master, token, nil
}

// Install 在安装时写入 Master/Token，并自动探测本机 1Panel 地址与用户名。
// 不启动长连接；运行时由 systemd 执行 "agent run"。
func Install(master, token string) error {
	if err := role.RefuseAgentIfMaster(); err != nil {
		return err
	}
	master = strings.TrimSpace(master)
	token = strings.TrimSpace(token)
	if master == "" || token == "" {
		return fmt.Errorf("master and token required")
	}
	if !strings.Contains(master, ":") {
		return fmt.Errorf("master must include port: %q", master)
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	AutofillPanel(cfg)
	cfg.Master = master
	cfg.Token = token
	return config.Save(cfg)
}

// InstallFromTarget 解析 host:port/token 并 Install。
func InstallFromTarget(target string) error {
	master, token, err := ParseInstallTarget(target)
	if err != nil {
		return err
	}
	return Install(master, token)
}
