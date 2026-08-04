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
	"strings"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/config"
	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

// handleUpdate 响应 Master 强制更新：从 Master 拉取 /agent.bin，替换本机二进制并重启服务。
func (c *Client) handleUpdate(stream *smux.Stream, body io.Reader) {
	_, _ = io.Copy(io.Discard, body)

	if err := c.downloadAndReplaceBinary(); err != nil {
		log.Printf("agent update failed: %v", err)
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}

	respMeta := &protocol.ResponseMeta{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	bodyJSON := fmt.Sprintf(`{"ok":true,"version":%q}`, buildinfo.Version)
	if err := protocol.WriteJSON(stream, respMeta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, strings.NewReader(bodyJSON))

	// 先回包再重启，避免 Master 读不到结果。
	go func() {
		time.Sleep(800 * time.Millisecond)
		cmd := exec.Command("systemctl", "restart", "1pm-agent.service")
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("agent restart after update: %v (%s)", err, string(out))
		}
	}()
}

func (c *Client) downloadAndReplaceBinary() error {
	if c.Cfg.Master == "" || c.Cfg.Token == "" {
		return fmt.Errorf("master/token missing")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign(c.Cfg.Token, ts)
	scheme := "http"
	if c.Cfg.MasterTLS {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/agent.bin?timestamp=%s&sign=%s", scheme, c.Cfg.Master, ts, sign)

	client := &http.Client{Timeout: 3 * time.Minute}
	if c.Cfg.MasterTLS {
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

	exe, err := os.Executable()
	if err != nil {
		exe = "/usr/local/bin/1pm"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
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
	log.Printf("agent binary updated from master %s (was %s)", c.Cfg.Master, buildinfo.Version)
	return nil
}
