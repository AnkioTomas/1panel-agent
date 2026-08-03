package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

// panelUpgradeInfo 对应 1Panel GET /api/v2/core/settings/upgrade 的 data。
type panelUpgradeInfo struct {
	TestVersion   string `json:"testVersion"`
	NewVersion    string `json:"newVersion"`
	LatestVersion string `json:"latestVersion"`
	ReleaseNote   string `json:"releaseNote"`
}

// handlePanelUpgrade 用 Agent 自持登录会话调用 1Panel 官方升级 API。
func (c *Client) handlePanelUpgrade(stream *smux.Stream, body io.Reader) {
	rawReq, _ := io.ReadAll(io.LimitReader(body, 4096))
	var req struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(rawReq, &req)

	result, err := c.upgradeLocalPanel(req.Version)
	if err != nil {
		log.Printf("panel upgrade failed: %v", err)
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}
	raw, _ := json.Marshal(result)
	respMeta := &protocol.ResponseMeta{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	if err := protocol.WriteJSON(stream, respMeta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, bytes.NewReader(raw))
}

type panelUpgradeResult struct {
	OK            bool   `json:"ok"`
	Skipped       bool   `json:"skipped,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	Message       string `json:"message,omitempty"`
}

func (c *Client) upgradeLocalPanel(forceVersion string) (*panelUpgradeResult, error) {
	base := strings.TrimRight(c.Cfg.PanelURL, "/")
	if base == "" {
		return nil, fmt.Errorf("panel_url missing")
	}
	cookies := c.getSessionCookies()
	if len(cookies) == 0 {
		return nil, fmt.Errorf("panel login required (no session)")
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	info, err := c.fetchUpgradeInfo(client, base, cookies)
	if err != nil {
		return nil, err
	}

	target := strings.TrimSpace(forceVersion)
	if target == "" {
		target = pickUpgradeVersion(info)
	}
	if target == "" {
		return &panelUpgradeResult{
			OK:      true,
			Skipped: true,
			Message: "already up to date",
		}, nil
	}

	if err := c.postPanelUpgrade(client, base, cookies, target); err != nil {
		return nil, err
	}
	// 升级会重启面板，清掉会话缓存。
	c.clearSession()
	return &panelUpgradeResult{
		OK:            true,
		TargetVersion: target,
		Message:       "upgrade started",
	}, nil
}

func pickUpgradeVersion(info *panelUpgradeInfo) string {
	for _, v := range []string{info.NewVersion, info.LatestVersion, info.TestVersion} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (c *Client) fetchUpgradeInfo(client *http.Client, base string, cookies []*http.Cookie) (*panelUpgradeInfo, error) {
	req, err := http.NewRequest(http.MethodGet, base+"/api/v2/core/settings/upgrade", nil)
	if err != nil {
		return nil, err
	}
	c.applyEntrance(req.Header)
	setCookieHeader(req, cookies)
	alignRequestCSRF(req.Header)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get upgrade info: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get upgrade info status %d: %s", resp.StatusCode, truncateRunes(string(raw), 200))
	}
	var ar struct {
		Code    int              `json:"code"`
		Message string           `json:"message"`
		Data    panelUpgradeInfo `json:"data"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("decode upgrade info: %w", err)
	}
	if ar.Code != 200 {
		return nil, fmt.Errorf("upgrade info: %s", ar.Message)
	}
	return &ar.Data, nil
}

func (c *Client) postPanelUpgrade(client *http.Client, base string, cookies []*http.Cookie, version string) error {
	body, _ := json.Marshal(map[string]string{"version": version})
	req, err := http.NewRequest(http.MethodPost, base+"/api/v2/core/settings/upgrade", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyEntrance(req.Header)
	setCookieHeader(req, cookies)
	alignRequestCSRF(req.Header)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post upgrade: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post upgrade status %d: %s", resp.StatusCode, truncateRunes(string(raw), 200))
	}
	var ar struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return fmt.Errorf("decode upgrade resp: %w body=%s", err, truncateRunes(string(raw), 200))
	}
	if ar.Code != 200 {
		return fmt.Errorf("upgrade failed: %s", ar.Message)
	}
	return nil
}

func setCookieHeader(req *http.Request, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	parts := make([]string, 0, len(cookies))
	for _, ck := range cookies {
		if ck == nil || ck.Name == "" {
			continue
		}
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	if len(parts) > 0 {
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
