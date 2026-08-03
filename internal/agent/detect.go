package agent

import (
	"fmt"
	"log"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

// AutofillPanel 通过 1Panel CLI 填充本机面板 URL、用户名与安全入口。
func AutofillPanel(cfg *config.Agent) {
	st, err := panel.ReadSettings()
	if err != nil {
		return
	}
	if cfg.PanelURL == "" || cfg.PanelURL == config.DefaultPanelURL {
		cfg.PanelURL = panel.LocalPanelURL(st.ServerPort)
	}
	if st.UserName != "" {
		cfg.PanelUser = st.UserName
	}
	cfg.PanelEntrance = st.SecurityEntrance
	log.Printf("detected local 1Panel %s user=%s entrance=%s", cfg.PanelURL, cfg.PanelUser, cfg.PanelEntrance)
}

// SetPassword 校验本机 1Panel 密码后加密落盘（供隧道侧自动登录）。
// 不先登录验证就存盘：装错密码只会在切节点时爆炸，而且会把 127.0.0.1 打进验证码锁定。
func SetPassword(plain string) error {
	if plain == "" {
		return fmt.Errorf("password required")
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	AutofillPanel(cfg)
	if cfg.PanelURL == "" || cfg.PanelUser == "" {
		return fmt.Errorf("无法探测本机 1Panel 用户（需要 1pctl/1panel）")
	}
	if _, err := panel.Login(cfg.PanelURL, cfg.PanelEntrance, cfg.PanelUser, plain); err != nil {
		return fmt.Errorf("密码验证失败（用户 %s @ %s）: %w", cfg.PanelUser, cfg.PanelURL, err)
	}
	enc, err := config.EncryptSecret(plain)
	if err != nil {
		return err
	}
	cfg.PanelPasswordEnc = enc
	cfg.PanelPassword = ""
	return config.Save(cfg)
}
