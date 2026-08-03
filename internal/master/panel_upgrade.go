package master

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"1panel-agent/internal/protocol"
)

// panelUpgradeResult 是单个 Agent 面板升级结果。
type panelUpgradeResult struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OK            bool   `json:"ok"`
	Skipped       bool   `json:"skipped,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
}

// handleUpgradePanel 通知所有在线 Agent：登录本机 1Panel 后调用官方升级 API。
func (s *Server) handleUpgradePanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)

	list := s.reg.List()
	results := make([]panelUpgradeResult, len(list))
	var wg sync.WaitGroup
	for i, a := range list {
		sess, ok := s.reg.Get(a.ID)
		if !ok {
			results[i] = panelUpgradeResult{ID: a.ID, Name: a.DisplayName(), OK: false, Error: "offline"}
			continue
		}
		wg.Add(1)
		go func(i int, info AgentInfo, sess *Session) {
			defer wg.Done()
			res := pushPanelUpgrade(sess, req.Version)
			res.ID = info.ID
			res.Name = info.DisplayName()
			results[i] = res
		}(i, a, sess)
	}
	wg.Wait()

	okN := 0
	for _, r := range results {
		if r.OK {
			okN++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total":   len(results),
		"ok":      okN,
		"results": results,
	})
}

func pushPanelUpgrade(sess *Session, version string) panelUpgradeResult {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		return panelUpgradeResult{OK: false, Error: "open stream: " + err.Error()}
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(12 * time.Minute))

	meta := &protocol.RequestMeta{
		Type:   protocol.StreamTypePanelUpgrade,
		Method: http.MethodPost,
		Path:   "/panel-upgrade",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		return panelUpgradeResult{OK: false, Error: err.Error()}
	}
	body, _ := json.Marshal(map[string]string{"version": version})
	if err := protocol.CopyChunks(stream, bytes.NewReader(body)); err != nil {
		return panelUpgradeResult{OK: false, Error: err.Error()}
	}

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		return panelUpgradeResult{OK: false, Error: "read response: " + err.Error()}
	}
	raw, err := io.ReadAll(protocol.NewChunkReader(stream))
	if err != nil {
		return panelUpgradeResult{OK: false, Error: "read body: " + err.Error()}
	}
	if respMeta.Status != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = fmt.Sprintf("status %d", respMeta.Status)
		}
		return panelUpgradeResult{OK: false, Error: msg}
	}
	var ar struct {
		OK            bool   `json:"ok"`
		Skipped       bool   `json:"skipped"`
		TargetVersion string `json:"target_version"`
		Message       string `json:"message"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return panelUpgradeResult{OK: false, Error: "bad json: " + err.Error()}
	}
	return panelUpgradeResult{
		OK:            ar.OK,
		Skipped:       ar.Skipped,
		TargetVersion: ar.TargetVersion,
		Message:       ar.Message,
	}
}
