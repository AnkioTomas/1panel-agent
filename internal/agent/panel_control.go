package agent

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

// handlePanelControl 处理 Master 下发的 master_tls 同步（本机面板 SSL 不由 Master 批量控制）。
func (c *Client) handlePanelControl(stream *smux.Stream, body io.Reader) {
	rawReq, _ := io.ReadAll(io.LimitReader(body, 4096))
	var req protocol.PanelControl
	if err := json.Unmarshal(rawReq, &req); err != nil {
		c.writeErr(stream, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "master_tls" {
		c.writeErr(stream, http.StatusBadRequest, "unsupported action: "+req.Action+" (agent local ssl is not controlled by master)")
		return
	}
	result, err := c.applyMasterTLS(req.Enable)
	if err != nil {
		log.Printf("panel control master_tls enable=%v failed: %v", req.Enable, err)
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}
	c.writeJSON(stream, result)
}

func (c *Client) applyMasterTLS(enable bool) (map[string]any, error) {
	if c.Cfg.MasterTLS == enable {
		return map[string]any{"ok": true, "enable": enable, "unchanged": true}, nil
	}
	c.Cfg.MasterTLS = enable
	if err := config.Save(c.Cfg); err != nil {
		return nil, err
	}
	log.Printf("master_tls set to %v; reconnecting", enable)
	go func() {
		time.Sleep(300 * time.Millisecond)
		c.closeSession()
	}()
	return map[string]any{"ok": true, "enable": enable}, nil
}
