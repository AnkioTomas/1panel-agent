package agent

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/release"

	"github.com/xtaci/smux"
)

// UpdateResult 是 Agent 自更新结果。
type UpdateResult struct {
	OldVersion string
	Tag        string
	Skipped    bool
	Restarting bool
}

// handleUpdate 响应 Master 强制更新信号：丢弃隧道 body（兼容旧版推包），
// 直接走与 `1pm update` 相同的 Release/CDN 自更新。
func (c *Client) handleUpdate(stream *smux.Stream, body io.Reader) {
	_ = stream.SetDeadline(time.Now().Add(10 * time.Minute))
	_, _ = io.Copy(io.Discard, body)

	res, err := UpdateSelf(false)
	if err != nil {
		log.Printf("agent update failed: %v", err)
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}

	c.writeJSON(stream, map[string]any{
		"ok":          true,
		"version":     buildinfo.Version,
		"tag":         res.Tag,
		"skipped":     res.Skipped,
		"old_version": res.OldVersion,
	})

	if !res.Skipped {
		go restartAgentUnit()
	}
}

// UpdateSelf 从 GitHub Release（含 CDN/镜像，与 install.sh / 1pm update 同源）拉取最新二进制。
// restart 为 true 时异步重启 agent 服务。
func UpdateSelf(restart bool) (*UpdateResult, error) {
	exe, err := currentExe()
	if err != nil {
		return nil, err
	}

	cfg := &release.Config{}
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

	tag, err = replaceAgentBinary(exe, cfg)
	if err != nil {
		return nil, err
	}
	log.Printf("agent binary updated to release %s (was %s) via api=%s dl=%s cdn=%s",
		tag, buildinfo.Version, cfg.GitHubAPI, cfg.GitHubDL, cfg.InstallCDN)

	res := &UpdateResult{
		OldVersion: buildinfo.Version,
		Tag:        tag,
		Restarting: restart,
	}
	if restart {
		go restartAgentUnit()
	}
	return res, nil
}

func replaceAgentBinary(exe string, cfg *release.Config) (string, error) {
	tmpDir, err := os.MkdirTemp(filepath.Dir(exe), "1pm-agent-update-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dl, err := cfg.DownloadBinary(tmpDir)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dl.Path, 0o755); err != nil {
		return "", fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(dl.Path, exe); err != nil {
		return "", fmt.Errorf("replace binary: %w", err)
	}
	return dl.Tag, nil
}

func restartAgentUnit() {
	time.Sleep(800 * time.Millisecond)
	cmd := exec.Command("systemctl", "restart", "1pm-agent.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("agent restart after update: %v (%s)", err, string(out))
	}
}

func currentExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "/usr/local/bin/1pm", nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}
