package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// GenerateToken returns a new random install/tunnel token.
func GenerateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

const masterFileName = "master.json"

type Master struct {
	Token         string `json:"token"`
	OriginalPort  int    `json:"original_port"`
	InternalPort  int    `json:"internal_port"`
	Entrance      string `json:"entrance"`
	PanelUser     string `json:"panel_user"`
	PanelPassword string `json:"panel_password"`
	PublicHost    string `json:"public_host"` // shown in register command, e.g. 10.211.55.14
}

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
