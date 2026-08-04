package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Agent 配置路径与默认值。
const (
	// DefaultPanelURL 是未探测到本机面板时的默认地址。
	DefaultPanelURL = "http://127.0.0.1:20560"
	dirName         = ".1panel-agent"
	fileName        = "agent.json"
)

// Agent 定义了 Agent 节点的配置结构。
// 隧道身份：Master + Token。面板地址/用户名/安全入口由 1panel CLI 自动探测；密码经 setpwd 加密存储。
type Agent struct {
	ID               string `json:"id"`
	Master           string `json:"master"` // host:port
	Token            string `json:"token"`
	MasterTLS        bool   `json:"master_tls,omitempty"` // Master 继承面板 SSL 时用 wss/https
	Name             string `json:"name,omitempty"`       // 展示名；空则用系统 hostname
	Group            string `json:"group,omitempty"`      // 分组；空则未分组
	PanelURL         string `json:"panel_url,omitempty"`
	PanelUser        string `json:"panel_user,omitempty"`
	PanelEntrance    string `json:"panel_entrance,omitempty"` // 安全入口路径段，自动探测
	PanelPasswordEnc string `json:"panel_password_enc,omitempty"`
	// PanelPassword 仅用于兼容旧版明文配置，Load 后会迁移为密文并清空。
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

// Load 从磁盘读取并解析 Agent 配置；必要时迁移明文密码。
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
	if err := migratePassword(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// migratePassword 将旧版明文 panel_password 加密落盘。
func migratePassword(cfg *Agent) error {
	if cfg.PanelPassword == "" || cfg.PanelPasswordEnc != "" {
		return nil
	}
	enc, err := EncryptSecret(cfg.PanelPassword)
	if err != nil {
		return err
	}
	cfg.PanelPasswordEnc = enc
	cfg.PanelPassword = ""
	return Save(cfg)
}

// Save 将 Agent 配置序列化并保存至磁盘（永不写回明文密码）。
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
	cfg.PanelPassword = "" // 强制不落明文
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

// PanelPasswordPlain 返回解密后的面板密码；未设置则返回空串。
func (cfg *Agent) PanelPasswordPlain() (string, error) {
	if cfg.PanelPasswordEnc == "" {
		return "", nil
	}
	return DecryptSecret(cfg.PanelPasswordEnc)
}

// newID 随机生成 16 进制字符串作为 Agent 节点 ID。
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SanitizeMeta 清洗节点名称/分组：去控制字符，限制长度。
func SanitizeMeta(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	const max = 64
	rs := []rune(out)
	if len(rs) > max {
		out = string(rs[:max])
	}
	return out
}
