package agent

import (
	"fmt"
	"log"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

// AutofillPanel reads local 1Panel listen address into agent config when possible.
func AutofillPanel(cfg *config.Agent) {
	st, err := panel.ReadSettings(panel.DefaultCoreDB)
	if err != nil {
		return
	}
	if cfg.PanelURL == "" || cfg.PanelURL == config.DefaultPanelURL {
		cfg.PanelURL = panel.LocalPanelURL(st.ServerPort)
	}
	log.Printf("detected local 1Panel %s", cfg.PanelURL)
}

func SetPanel(panelURL, panelKey string) error {
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
	if cfg.PanelURL == "" {
		return fmt.Errorf("panel-url required")
	}
	return config.Save(cfg)
}
