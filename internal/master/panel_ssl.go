package master

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"
)

// handleUpgradePanelMaster 用当前浏览器面板会话触发本机 1Panel 官方升级。
func (s *Server) handleUpgradePanelMaster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)

	cookies := r.Cookies()
	client := panel.NewInsecureClient(12 * time.Minute)
	res, err := panel.Upgrade(client, s.LocalPanel, s.Entrance, cookies, req.Version)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// panelSSLResult 是批量 SSL 中单个节点结果。
type panelSSLResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Action  string `json:"action,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// handlePanelSSL 批量开关主节点 + 在线子节点的面板自签 SSL，并同步 master_tls。
func (s *Server) handlePanelSSL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	domain := hostOnly(s.AdvertiseHost(r))
	cookies := r.Cookies()
	client := panel.NewInsecureClient(2 * time.Minute)

	out := map[string]any{
		"enable": req.Enable,
	}
	var masterErr string

	if req.Enable {
		if err := panel.UpdateSSL(client, s.LocalPanel, s.Entrance, cookies, true, domain); err != nil {
			masterErr = err.Error()
		} else if err := s.waitPanelSSL(true, 90*time.Second); err != nil {
			masterErr = err.Error()
		}
		out["master_ssl"] = panelSSLResult{
			ID: "local", Name: "主节点", OK: masterErr == "", Action: "ssl",
			Error: masterErr,
		}
		if masterErr != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		tlsResults := s.broadcastPanelControl(protocol.PanelControl{Action: "master_tls", Enable: true})
		sslResults := s.broadcastPanelControl(protocol.PanelControl{Action: "ssl", Enable: true, Domain: domain})
		out["master_tls"] = tlsResults
		out["agents_ssl"] = sslResults
		out["ok"] = countOK(tlsResults) == len(tlsResults) && countOK(sslResults) == len(sslResults)
	} else {
		if err := panel.UpdateSSL(client, s.LocalPanel, s.Entrance, cookies, false, domain); err != nil {
			masterErr = err.Error()
		} else if err := s.waitPanelSSL(false, 90*time.Second); err != nil {
			masterErr = err.Error()
		}
		out["master_ssl"] = panelSSLResult{
			ID: "local", Name: "主节点", OK: masterErr == "", Action: "ssl",
			Error: masterErr,
		}
		if masterErr != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		tlsResults := s.broadcastPanelControl(protocol.PanelControl{Action: "master_tls", Enable: false})
		sslResults := s.broadcastPanelControl(protocol.PanelControl{Action: "ssl", Enable: false, Domain: domain})
		out["master_tls"] = tlsResults
		out["agents_ssl"] = sslResults
		out["ok"] = countOK(tlsResults) == len(tlsResults) && countOK(sslResults) == len(sslResults)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func countOK(list []panelSSLResult) int {
	n := 0
	for _, r := range list {
		if r.OK {
			n++
		}
	}
	return n
}

func (s *Server) waitPanelSSL(wantReady bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ready := panel.PanelSSLReady()
		if ready == wantReady {
			// 给 reloader / 上游切换留一点时间
			time.Sleep(1500 * time.Millisecond)
			if panel.PanelSSLReady() == wantReady {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	if wantReady {
		return fmt.Errorf("timeout waiting panel SSL cert ready")
	}
	return fmt.Errorf("timeout waiting panel SSL cert removed")
}

func (s *Server) broadcastPanelControl(ctrl protocol.PanelControl) []panelSSLResult {
	list := s.reg.List()
	results := make([]panelSSLResult, len(list))
	var wg sync.WaitGroup
	for i, a := range list {
		sess, ok := s.reg.Get(a.ID)
		if !ok {
			results[i] = panelSSLResult{ID: a.ID, Name: a.DisplayName(), OK: false, Action: ctrl.Action, Error: "offline"}
			continue
		}
		wg.Add(1)
		go func(i int, info AgentInfo, sess *Session) {
			defer wg.Done()
			res := pushPanelControl(sess, ctrl)
			res.ID = info.ID
			res.Name = info.DisplayName()
			results[i] = res
		}(i, a, sess)
	}
	wg.Wait()
	return results
}

func pushPanelControl(sess *Session, ctrl protocol.PanelControl) panelSSLResult {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		return panelSSLResult{OK: false, Action: ctrl.Action, Error: "open stream: " + err.Error()}
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(3 * time.Minute))

	meta := &protocol.RequestMeta{
		Type:   protocol.StreamTypePanelControl,
		Method: http.MethodPost,
		Path:   "/panel-control",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		return panelSSLResult{OK: false, Action: ctrl.Action, Error: err.Error()}
	}
	body, _ := json.Marshal(ctrl)
	if err := protocol.CopyChunks(stream, bytes.NewReader(body)); err != nil {
		return panelSSLResult{OK: false, Action: ctrl.Action, Error: err.Error()}
	}

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		return panelSSLResult{OK: false, Action: ctrl.Action, Error: "read response: " + err.Error()}
	}
	raw, err := io.ReadAll(protocol.NewChunkReader(stream))
	if err != nil {
		return panelSSLResult{OK: false, Action: ctrl.Action, Error: "read body: " + err.Error()}
	}
	if respMeta.Status != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = fmt.Sprintf("status %d", respMeta.Status)
		}
		return panelSSLResult{OK: false, Action: ctrl.Action, Error: msg}
	}
	return panelSSLResult{OK: true, Action: ctrl.Action, Message: strings.TrimSpace(string(raw))}
}

func hostOnly(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "127.0.0.1"
	}
	h, _, err := net.SplitHostPort(hostport)
	if err == nil && h != "" {
		return h
	}
	// 可能无端口
	return hostport
}
