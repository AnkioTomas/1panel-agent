package agent

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

// handlePanelControl 处理 Master 下发的面板控制指令：master_tls 同步 / session 刷新。
func (c *Client) handlePanelControl(stream *smux.Stream, body io.Reader) {
	rawReq, _ := io.ReadAll(io.LimitReader(body, 4096))
	var req protocol.PanelControl
	if err := json.Unmarshal(rawReq, &req); err != nil {
		c.writeErr(stream, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "master_tls":
		result, err := c.applyMasterTLS(req.Enable)
		if err != nil {
			log.Printf("panel control master_tls enable=%v failed: %v", req.Enable, err)
			c.writeErr(stream, http.StatusBadGateway, err.Error())
			return
		}
		c.writeJSON(stream, result)
	case "refresh":
		result, err := c.refreshPanelSession()
		if err != nil {
			log.Printf("panel session refresh failed: %v", err)
			c.writeErr(stream, http.StatusBadGateway, err.Error())
			return
		}
		c.writeJSON(stream, result)
	default:
		c.writeErr(stream, http.StatusBadRequest, "unsupported action: "+req.Action)
		return
	}
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

// refreshPanelSession 清除缓存的会话 Cookie，触发重新登录 1Panel，返回新 cookie 数量。
func (c *Client) refreshPanelSession() (map[string]any, error) {
	pass, err := c.Cfg.PanelPasswordPlain()
	if err != nil || pass == "" || c.Cfg.PanelUser == "" {
		return map[string]any{"ok": false, "error": "no panel credentials"}, nil
	}
	c.clearSession()
	res, err := panel.Login(c.Cfg.PanelURL, c.Cfg.PanelEntrance, c.Cfg.PanelUser, pass)
	if err != nil {
		log.Printf("agent refresh-panel: login failed: %v", err)
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	cookies := res.Cookies
	c.sessMu.Lock()
	c.sessCookies = cookies
	c.sessUntil = time.Now().Add(10 * time.Minute)
	c.sessMu.Unlock()
	log.Printf("agent refresh-panel: ok (cookies=%d entrance=%q)", len(cookies), c.Cfg.PanelEntrance)
	return map[string]any{"ok": true, "cookies": len(cookies)}, nil
}
