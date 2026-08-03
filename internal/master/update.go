package master

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/protocol"
)

// updateResult 是单个 Agent 强制更新的结果。
type updateResult struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// handleForceUpdate 通知所有在线 Agent 从本 Master 拉取 /agent.bin 并重启。
func (s *Server) handleForceUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	list := s.reg.List()
	results := make([]updateResult, len(list))
	var wg sync.WaitGroup
	for i, a := range list {
		sess, ok := s.reg.Get(a.ID)
		if !ok {
			results[i] = updateResult{ID: a.ID, Name: a.DisplayName(), OK: false, Error: "offline"}
			continue
		}
		wg.Add(1)
		go func(i int, info AgentInfo, sess *Session) {
			defer wg.Done()
			err := pushAgentUpdate(sess)
			res := updateResult{ID: info.ID, Name: info.DisplayName(), OK: err == nil}
			if err != nil {
				res.Error = err.Error()
			}
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
		"master_version": buildinfo.Version,
		"total":          len(results),
		"ok":             okN,
		"results":        results,
	})
}

func pushAgentUpdate(sess *Session) error {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(4 * time.Minute))

	meta := &protocol.RequestMeta{
		Type:   protocol.StreamTypeUpdate,
		Method: http.MethodPost,
		Path:   "/update",
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		return err
	}
	if err := protocol.CopyChunks(stream, http.NoBody); err != nil {
		return err
	}

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	raw, err := io.ReadAll(protocol.NewChunkReader(stream))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if respMeta.Status != http.StatusOK {
		msg := string(raw)
		if msg == "" {
			msg = fmt.Sprintf("status %d", respMeta.Status)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
