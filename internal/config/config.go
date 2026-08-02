package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	DefaultPanelURL = "http://127.0.0.1:20560"
	dirName         = ".1panel-agent"
	fileName        = "agent.json"
)

type Agent struct {
	ID       string `json:"id"`
	Master   string `json:"master"` // host:port
	Token    string `json:"token"`
	PanelURL string `json:"panel_url"`
	PanelKey string `json:"panel_key,omitempty"`
}

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

func Load() (*Agent, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Agent
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.PanelURL == "" {
		cfg.PanelURL = DefaultPanelURL
	}
	return &cfg, nil
}

func Save(cfg *Agent) error {
	if cfg.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		cfg.ID = id
	}
	if cfg.PanelURL == "" {
		cfg.PanelURL = DefaultPanelURL
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func LoadOrEmpty() (*Agent, error) {
	cfg, err := Load()
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return &Agent{PanelURL: DefaultPanelURL}, nil
	}
	return nil, err
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
