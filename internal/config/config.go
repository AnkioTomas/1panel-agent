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

// Agent 定义了 Agent 节点的配置结构。
type Agent struct {
	ID            string `json:"id"`
	Master        string `json:"master"` // host:port
	Token         string `json:"token"`
	PanelURL      string `json:"panel_url"`
	PanelKey      string `json:"panel_key,omitempty"`
	PanelUser     string `json:"panel_user,omitempty"`
	PanelPassword string `json:"panel_password,omitempty"`
}

// Dir 返回 Agent 配置目录路径（~/.1panel-agent）。
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName), nil
}

// Path 返回 Agent 配置文件路径（~/.1panel-agent/agent.json）。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load 从磁盘读取并解析 Agent 配置。
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

// Save 将 Agent 配置序列化并保存至磁盘。
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

// LoadOrEmpty 读取 Agent 配置；若文件不存在则返回默认空配置。
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

// newID 随机生成 16 进制字符串作为 Agent 节点 ID。
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
