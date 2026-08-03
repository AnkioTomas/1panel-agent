package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// GenerateToken 生成一个随机的通信注册 Token。
func GenerateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// masterFileName 是 Master 状态文件名。
const masterFileName = "master.json"

// Master 定义了 Master 节点的配置结构。
type Master struct {
	Token        string `json:"token"`
	OriginalPort int    `json:"original_port"`
	InternalPort int    `json:"internal_port"`
	PublicHost   string `json:"public_host,omitempty"` // optional NAT override; default = request Host
}

// MasterPath 返回 Master 配置文件路径（root 用户下默认为 /var/lib/1pm/master.json）。
func MasterPath() (string, error) {
	// Prefer system path when root, else home.
	if os.Geteuid() == 0 {
		return "/var/lib/1pm/master.json", nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, masterFileName), nil
}

// LoadMaster 从磁盘读取 Master 配置。
func LoadMaster() (*Master, error) {
	path, err := MasterPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Master
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveMaster 将 Master 配置保存至磁盘。
func SaveMaster(cfg *Master) error {
	path, err := MasterPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadMasterOrEmpty 读取 Master 配置；若文件不存在则返回默认空配置。
func LoadMasterOrEmpty() (*Master, error) {
	cfg, err := LoadMaster()
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return &Master{}, nil
	}
	return nil, err
}
