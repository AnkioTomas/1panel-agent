package agent

import (
	"fmt"
	"log"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

// AutofillPanel 尝试通过 1Panel CLI 读取本地 1Panel 端口并自动填充到配置中。
func AutofillPanel(cfg *config.Agent) {
	st, err := panel.ReadSettings()
	if err != nil {
		return
	}
	if cfg.PanelURL == "" || cfg.PanelURL == config.DefaultPanelURL {
		cfg.PanelURL = panel.LocalPanelURL(st.ServerPort)
	}
	log.Printf("detected local 1Panel %s", cfg.PanelURL)
}

// SetPanel 更新并保存 Agent 的面板配置（URL、Key、User、Password）。
func SetPanel(panelURL, panelKey, panelUser, panelPassword string) error {
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	AutofillPanel(cfg)
	if panelURL != "" {
		cfg.PanelURL = panelURL
	}
	if panelKey != "" {
		cfg.PanelKey = panelKey
	}
	if panelUser != "" {
		cfg.PanelUser = panelUser
	}
	if panelPassword != "" {
		cfg.PanelPassword = panelPassword
	}
	if cfg.PanelURL == "" {
		return fmt.Errorf("panel-url required")
	}
	return config.Save(cfg)
}
