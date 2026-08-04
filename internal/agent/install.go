package agent

import (
	"fmt"
	"strings"

	"1panel-agent/internal/config"
	"1panel-agent/internal/role"
)

// InstallOpts 是安装时可选的节点展示元数据。
type InstallOpts struct {
	Name      string
	Group     string
	MasterTLS bool
}

// Install 在安装时写入 Master/Token，并自动探测本机 1Panel 地址与用户名。
// 不启动长连接；运行时由 systemd 执行 "agent run"。
func Install(master, token string, opts InstallOpts) error {
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
	if err := AutofillPanel(cfg); err != nil {
		return err
	}
	cfg.Master = master
	cfg.Token = token
	cfg.MasterTLS = opts.MasterTLS
	cfg.Name = config.SanitizeMeta(opts.Name)
	cfg.Group = config.SanitizeMeta(opts.Group)
	return config.Save(cfg)
}
