package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

// handlePanelControl 处理 Master 下发的本机 SSL / master_tls 控制。
func (c *Client) handlePanelControl(stream *smux.Stream, body io.Reader) {
	rawReq, _ := io.ReadAll(io.LimitReader(body, 4096))
	var req protocol.PanelControl
	if err := json.Unmarshal(rawReq, &req); err != nil {
		c.writeErr(stream, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	var (
		result any
		err    error
	)
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "ssl":
		result, err = c.applyLocalSSL(req.Enable, req.Domain)
	case "master_tls":
		result, err = c.applyMasterTLS(req.Enable)
	default:
		c.writeErr(stream, http.StatusBadRequest, "unknown action: "+req.Action)
		return
	}
	if err != nil {
		log.Printf("panel control %s enable=%v failed: %v", req.Action, req.Enable, err)
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

func (c *Client) applyLocalSSL(enable bool, domain string) (map[string]any, error) {
	cookies := c.getSessionCookies()
	if len(cookies) == 0 {
		return nil, fmt.Errorf("panel login required (no session)")
	}
	if domain == "" {
		domain = sslDomainHint()
	}
	client := panel.NewInsecureClient(2 * time.Minute)
	if err := panel.UpdateSSL(client, c.Cfg.PanelURL, c.Cfg.PanelEntrance, cookies, enable, domain); err != nil {
		return nil, err
	}
	c.clearSession()
	// 面板重启后 scheme 可能变化，重新探测。
	AutofillPanel(c.Cfg)
	_ = config.Save(c.Cfg)
	return map[string]any{
		"ok":     true,
		"enable": enable,
		"domain": domain,
	}, nil
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
	// 回包后异步关掉当前 smux，逼 Run 重连。
	go func() {
		time.Sleep(300 * time.Millisecond)
		c.closeSession()
	}()
	return map[string]any{"ok": true, "enable": enable}, nil
}

func sslDomainHint() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return "127.0.0.1"
}
