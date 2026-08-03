package master

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"1panel-agent/internal/protocol"
)

// refreshAgentStats 向在线 Agent 拉取 CPU/内存/版本并写回 Registry。
func (s *Server) refreshAgentStats() {
	list := s.reg.List()
	var wg sync.WaitGroup
	for _, a := range list {
		sess, ok := s.reg.Get(a.ID)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(id string, sess *Session) {
			defer wg.Done()
			st, err := fetchAgentStats(sess)
			if err != nil {
				return
			}
			s.reg.UpdateStats(id, st.CPUPercent, st.MemTotal, st.MemUsed, st.AgentVersion, st.PanelVersion)
		}(a.ID, sess)
	}
	wg.Wait()
}

func fetchAgentStats(sess *Session) (*protocol.HostStats, error) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(3 * time.Second))

	meta := &protocol.RequestMeta{
		Type:   protocol.StreamTypeStats,
		Method: http.MethodGet,
		Path:   "/stats",
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		return nil, err
	}
	if err := protocol.CopyChunks(stream, http.NoBody); err != nil {
		return nil, err
	}
	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(protocol.NewChunkReader(stream))
	if err != nil {
		return nil, err
	}
	var st protocol.HostStats
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
