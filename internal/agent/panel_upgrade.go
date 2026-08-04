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

	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

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

func (c *Client) upgradeLocalPanel(forceVersion string) (*panel.UpgradeResult, error) {
	base := strings.TrimRight(c.Cfg.PanelURL, "/")
	if base == "" {
		return nil, fmt.Errorf("panel_url missing")
	}
	cookies := c.getSessionCookies()
	if len(cookies) == 0 {
		return nil, fmt.Errorf("panel login required (no session)")
	}

	res, err := panel.Upgrade(panel.NewInsecureClient(10*time.Minute), base, c.Cfg.PanelEntrance, cookies, forceVersion)
	if err != nil {
		return nil, err
	}
	if !res.Skipped {
		// 升级会重启面板，清掉会话缓存。
		c.clearSession()
	}
	return res, nil
}
