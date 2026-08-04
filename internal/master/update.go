package master

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// handleForceUpdate 经隧道把本机二进制推给所有在线 Agent，替换后重启。
// 不再让 Agent 回头 HTTP 拉 /agent.bin——那会空置 smux 流，慢链路/NAT 下必超时。
func (s *Server) handleForceUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f, size, err := openSelfBinary()
	if err != nil {
		http.Error(w, "open binary: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = f.Close()

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
			// 每条流各自打开文件，避免并发达竞争同一 *os.File 游标。
			err := pushAgentUpdate(sess, size, openSelfBinaryReader)
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

func openSelfBinary() (*os.File, int64, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, 0, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	f, err := os.Open(exe)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func openSelfBinaryReader() (io.ReadCloser, error) {
	f, _, err := openSelfBinary()
	if err != nil {
		return nil, err
	}
	return f, nil
}

func pushAgentUpdate(sess *Session, size int64, open func() (io.ReadCloser, error)) error {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(5 * time.Minute))

	meta := &protocol.RequestMeta{
		Type:   protocol.StreamTypeUpdate,
		Method: http.MethodPost,
		Path:   "/update",
		Headers: map[string][]string{
			"Content-Type":   {"application/octet-stream"},
			"Content-Length": {fmt.Sprintf("%d", size)},
		},
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		return err
	}

	r, err := open()
	if err != nil {
		return fmt.Errorf("open binary: %w", err)
	}
	defer r.Close()
	if err := protocol.CopyChunks(stream, r); err != nil {
		return fmt.Errorf("send binary: %w", err)
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
