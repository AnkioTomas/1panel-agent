package agent

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/config"

	"github.com/xtaci/smux"
)

// handleUpdate 响应 Master 强制更新：优先从隧道 body 接收二进制；
// body 为空则回退 HTTP 拉 /agent.bin（兼容旧 Master）。
func (c *Client) handleUpdate(stream *smux.Stream, body io.Reader) {
	_ = stream.SetDeadline(time.Now().Add(10 * time.Minute))
	if err := c.replaceBinaryFrom(body); err != nil {
		log.Printf("agent update failed: %v", err)
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}

	c.writeJSON(stream, map[string]any{"ok": true, "version": buildinfo.Version})

	// 先回包再重启，避免 Master 读不到结果。
	go restartAgentUnit()
}

// UpdateSelf 从已配置 Master 拉取 /agent.bin，替换本机二进制；restart 为 true 时异步重启 agent 服务。
func UpdateSelf(restart bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w (run agent install first)", err)
	}
	if err := downloadAndReplaceBinary(cfg); err != nil {
		return err
	}
	if restart {
		go restartAgentUnit()
	}
	return nil
}

func restartAgentUnit() {
	time.Sleep(800 * time.Millisecond)
	cmd := exec.Command("systemctl", "restart", "1pm-agent.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("agent restart after update: %v (%s)", err, string(out))
	}
}

// replaceBinaryFrom 把 r 写成可执行文件并替换本机二进制。
// r 读到 0 字节时回退到 HTTP 下载（旧 Master 只发空 body）。
func (c *Client) replaceBinaryFrom(r io.Reader) error {
	exe, err := currentExe()
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "1pm-update-*")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	n, err := io.Copy(tmp, r)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if n == 0 {
		_ = os.Remove(tmpName)
		return downloadAndReplaceBinary(c.Cfg)
	}

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	log.Printf("agent binary updated via tunnel (%d bytes, was %s)", n, buildinfo.Version)
	return nil
}

func downloadAndReplaceBinary(cfg *config.Agent) error {
	if cfg == nil || cfg.Master == "" || cfg.Token == "" {
		return fmt.Errorf("master/token missing")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign(cfg.Token, ts)
	scheme := "http"
	if cfg.MasterTLS {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/agent.bin?timestamp=%s&sign=%s", scheme, cfg.Master, ts, sign)

	client := &http.Client{Timeout: 3 * time.Minute}
	if cfg.MasterTLS {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("download status %d: %s", resp.StatusCode, string(b))
	}

	exe, err := currentExe()
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "1pm-update-*")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	log.Printf("agent binary updated from master %s (was %s)", cfg.Master, buildinfo.Version)
	return nil
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
