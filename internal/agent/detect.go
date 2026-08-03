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

// SetPassword 加密保存本机 1Panel 密码（供隧道侧自动登录）。
func SetPassword(plain string) error {
	if plain == "" {
		return fmt.Errorf("password required")
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	AutofillPanel(cfg)
	enc, err := config.EncryptSecret(plain)
	if err != nil {
		return err
	}
	cfg.PanelPasswordEnc = enc
	cfg.PanelPassword = ""
	return config.Save(cfg)
}
