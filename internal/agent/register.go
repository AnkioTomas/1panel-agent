package agent

import (
	"fmt"
	"strings"

	"1panel-agent/internal/config"
)

// ParseRegisterTarget parses "ip:port/token" or "host:port/token".
func ParseRegisterTarget(s string) (master, token string, err error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "ws://")
	s = strings.TrimPrefix(s, "wss://")

	idx := strings.LastIndex(s, "/")
	if idx <= 0 || idx == len(s)-1 {
		return "", "", fmt.Errorf("invalid register target %q; want host:port/token", s)
	}
	master = s[:idx]
	token = s[idx+1:]
	if master == "" || token == "" {
		return "", "", fmt.Errorf("invalid register target %q; want host:port/token", s)
	}
	if !strings.Contains(master, ":") {
		return "", "", fmt.Errorf("master must include port: %q", master)
	}
	return master, token, nil
}

func RegisterAndRun(target string) error {
	master, token, err := ParseRegisterTarget(target)
	if err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	AutofillPanel(cfg)
	cfg.Master = master
	cfg.Token = token
	if err := config.Save(cfg); err != nil {
		return err
	}
	return Run(cfg)
}
