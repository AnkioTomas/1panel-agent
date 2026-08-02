package agent

import (
	"fmt"
	"log"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

// AutofillPanel reads local 1Panel settings into agent config when possible.
func AutofillPanel(cfg *config.Agent) {
	st, err := panel.ReadSettings(panel.DefaultCoreDB)
	if err != nil {
		return
	}
	if cfg.PanelURL == "" || cfg.PanelURL == config.DefaultPanelURL {
		cfg.PanelURL = panel.LocalPanelURL(st.ServerPort)
	}
	if cfg.PanelEntrance == "" {
		cfg.PanelEntrance = st.SecurityEntrance
	}
	if cfg.PanelUser == "" {
		cfg.PanelUser = st.UserName
	}
	log.Printf("detected local 1Panel %s entrance=%s", cfg.PanelURL, cfg.PanelEntrance)
}

func SetPanel(panelURL, panelKey, panelUser, panelPass, entrance string) error {
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
	if panelPass != "" {
		cfg.PanelPassword = panelPass
	}
	if entrance != "" {
		cfg.PanelEntrance = entrance
	}
	if cfg.PanelURL == "" {
		return fmt.Errorf("panel-url required")
	}
	return config.Save(cfg)
}
