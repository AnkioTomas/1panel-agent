package master

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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

type forceUpdateStatus struct {
	Running bool           `json:"running"`
	Total   int            `json:"total"`
	OK      int            `json:"ok"`
	Results []updateResult `json:"results,omitempty"`
	Error   string         `json:"error,omitempty"`
	Started time.Time      `json:"started,omitempty"`
	DoneAt  time.Time      `json:"done_at,omitempty"`
}

// handleForceUpdate 异步经隧道推送本机二进制。
// POST：立刻返回 accepted，后台推送（避免浏览器/反代把长传当成超时）。
// GET：查询最近一次任务状态。
func (s *Server) handleForceUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeForceUpdateStatus(w)
		return
	case http.MethodPost:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	list := s.reg.List()
	jobs := make([]struct {
		info AgentInfo
		sess *Session
	}, 0, len(list))
	offline := make([]updateResult, 0)
	for _, a := range list {
		sess, ok := s.reg.Get(a.ID)
		if !ok {
			offline = append(offline, updateResult{ID: a.ID, Name: a.DisplayName(), OK: false, Error: "offline"})
			continue
		}
		jobs = append(jobs, struct {
			info AgentInfo
			sess *Session
		}{a, sess})
	}

	s.updateMu.Lock()
	if s.updateStatus.Running {
		st := s.snapshotForceUpdateLocked()
		s.updateMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      false,
			"message": "update already running",
			"status":  st,
		})
		return
	}
	s.updateStatus = forceUpdateStatus{
		Running: true,
		Total:   len(jobs) + len(offline),
		Started: time.Now(),
		Results: append([]updateResult{}, offline...),
	}
	s.updateMu.Unlock()

	go s.runForceUpdate(jobs)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"accepted":       true,
		"master_version": buildinfo.Version,
		"total":          len(jobs) + len(offline),
		"dispatched":     len(jobs),
		"message":        "update dispatched; poll GET /__mp/api/force-update",
	})
}

func (s *Server) writeForceUpdateStatus(w http.ResponseWriter) {
	s.updateMu.Lock()
	st := s.snapshotForceUpdateLocked()
	s.updateMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (s *Server) snapshotForceUpdateLocked() forceUpdateStatus {
	out := s.updateStatus
	if out.Results != nil {
		cp := make([]updateResult, len(out.Results))
		copy(cp, out.Results)
		out.Results = cp
	}
	return out
}

func (s *Server) runForceUpdate(jobs []struct {
	info AgentInfo
	sess *Session
}) {
	defer func() {
		s.updateMu.Lock()
		s.updateStatus.Running = false
		s.updateStatus.DoneAt = time.Now()
		okN := 0
		for _, r := range s.updateStatus.Results {
			if r.OK {
				okN++
			}
		}
		s.updateStatus.OK = okN
		s.updateMu.Unlock()
	}()

	f, size, err := openSelfBinary()
	if err != nil {
		s.updateMu.Lock()
		s.updateStatus.Error = "open binary: " + err.Error()
		s.updateStatus.Running = false
		s.updateStatus.DoneAt = time.Now()
		s.updateMu.Unlock()
		log.Printf("force-update: %v", err)
		return
	}
	_ = f.Close()

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(info AgentInfo, sess *Session) {
			defer wg.Done()
			err := pushAgentUpdate(sess, size, openSelfBinaryReader)
			res := updateResult{ID: info.ID, Name: info.DisplayName(), OK: err == nil}
			if err != nil {
				res.Error = err.Error()
				log.Printf("force-update %s (%s): %v", info.DisplayName(), info.ID, err)
			} else {
				log.Printf("force-update %s (%s): ok", info.DisplayName(), info.ID)
			}
			s.updateMu.Lock()
			s.updateStatus.Results = append(s.updateStatus.Results, res)
			s.updateMu.Unlock()
		}(job.info, job.sess)
	}
	wg.Wait()
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
	// 大二进制 + 慢链路：读写共用绝对 deadline，期间靠 smux keepalive 保活会话。
	_ = stream.SetDeadline(time.Now().Add(10 * time.Minute))

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
