package master

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/config"
	"1panel-agent/internal/release"
)

// UpdateResult 是本机 Master 自更新结果。
type UpdateResult struct {
	OldVersion string
	Tag        string
	Skipped    bool
	Restarting bool
}

// UpdateSelf 从 Release 拉取最新 1pm，替换本机二进制；restart 为 true 时异步重启 master 服务。
func UpdateSelf(restart bool) (*UpdateResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	cfg := releaseConfigFromState()
	tag, err := cfg.ResolveTag()
	if err != nil {
		return nil, err
	}
	if tag == buildinfo.Version {
		return &UpdateResult{
			OldVersion: buildinfo.Version,
			Tag:        tag,
			Skipped:    true,
		}, nil
	}

	tag, err = replaceMasterBinary(exe, cfg)
	if err != nil {
		return nil, err
	}
	log.Printf("master binary updated to release %s (was %s) via api=%s dl=%s cdn=%s",
		tag, buildinfo.Version, cfg.GitHubAPI, cfg.GitHubDL, cfg.InstallCDN)

	res := &UpdateResult{
		OldVersion: buildinfo.Version,
		Tag:        tag,
		Restarting: restart,
	}
	if restart {
		go restartUnit("1pm-master.service")
	}
	return res, nil
}

// handleUpdateMaster 从 Release 拉取最新 1pm，替换本机二进制并异步重启 master。
func (s *Server) handleUpdateMaster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	res, err := UpdateSelf(true)
	if err != nil {
		writeUpdateMasterErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if res.Skipped {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"skipped": true,
			"tag":     res.Tag,
			"message": "already up to date",
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"old_version": res.OldVersion,
		"tag":         res.Tag,
		"restarting":  true,
	})
}

// releaseConfigFromState 优先用 master.json 里安装时保存的源，其次环境变量，避免默默打公网。
func releaseConfigFromState() *release.Config {
	cfg := &release.Config{}
	if state, err := config.LoadMaster(); err == nil {
		cfg.GitHubAPI = state.GitHubAPI
		cfg.GitHubDL = state.GitHubDL
		cfg.InstallCDN = state.InstallCDN
	}
	return cfg
}

// syncReleaseSourceFromEnv 把安装/运行环境中的 GITHUB_* / INSTALL_CDN 写入 master.json。
func syncReleaseSourceFromEnv(state *config.Master) bool {
	if state == nil {
		return false
	}
	changed := false
	if v := os.Getenv("GITHUB_API"); v != "" && state.GitHubAPI != v {
		state.GitHubAPI = v
		changed = true
	}
	if v := os.Getenv("GITHUB_DL"); v != "" && state.GitHubDL != v {
		state.GitHubDL = v
		changed = true
	}
	if v := os.Getenv("INSTALL_CDN"); v != "" && state.InstallCDN != v {
		state.InstallCDN = v
		changed = true
	}
	return changed
}

// replaceMasterBinary 下载 Release 二进制并 rename 覆盖 exe。
func replaceMasterBinary(exe string, cfg *release.Config) (tag string, err error) {
	tmpDir, err := os.MkdirTemp(filepath.Dir(exe), "1pm-master-update-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	res, err := cfg.DownloadBinary(tmpDir)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(res.Path, 0o755); err != nil {
		return "", fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(res.Path, exe); err != nil {
		return "", fmt.Errorf("replace binary: %w", err)
	}
	return res.Tag, nil
}

func restartUnit(unit string) {
	time.Sleep(800 * time.Millisecond)
	cmd := exec.Command("systemctl", "restart", unit)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("%s restart after update: %v (%s)", unit, err, string(out))
	}
}

func writeUpdateMasterErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      false,
		"message": msg,
	})
}
