package master

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"1panel-agent/internal/protocol"
)

// handleSwitch 校验 Agent 在线后设置 mp_node Cookie 并重定向到首页。
// 不篡改或暂存主节点登录态 Cookie，避免多节点间切换覆盖与状态踩踏。
func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	if _, ok := s.reg.Get(id); !ok {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    id,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLocal 切回主节点 1Panel，清除 mp_node Cookie。
func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// warmAgentSession 经隧道打一个需登录的 API，触发 Agent 自动登录，返回会话 Cookie。
func (s *Server) warmAgentSession(sess *Session) ([]*http.Cookie, error) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	meta := &protocol.RequestMeta{
		Type:   protocol.StreamTypeHTTP,
		Method: http.MethodGet,
		Path:   "/api/v2/dashboard/base/os",
		Headers: map[string][]string{
			"Accept": {"application/json"},
		},
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
	_, _ = io.Copy(io.Discard, protocol.NewChunkReader(stream))

	cookies := parseSetCookies(respMeta.Headers["Set-Cookie"])
	hasSession := false
	for _, c := range cookies {
		if strings.EqualFold(c.Name, "psession") && c.Value != "" {
			hasSession = true
			break
		}
	}
	if !hasSession {
		return nil, fmt.Errorf("no psession in agent response (is panel password set?)")
	}
	if respMeta.Status != http.StatusOK {
		return nil, fmt.Errorf("agent warm status %d", respMeta.Status)
	}
	return cookies, nil
}

func parseSetCookies(vals []string) []*http.Cookie {
	var out []*http.Cookie
	for _, v := range vals {
		if c := parseOneSetCookie(v); c != nil {
			out = append(out, c)
		}
	}
	return out
}

func parseOneSetCookie(raw string) *http.Cookie {
	parts := strings.Split(raw, ";")
	if len(parts) == 0 {
		return nil
	}
	nv := strings.TrimSpace(parts[0])
	name, value, ok := strings.Cut(nv, "=")
	if !ok || name == "" {
		return nil
	}
	c := &http.Cookie{Name: name, Value: value, Path: "/"}
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		switch {
		case strings.EqualFold(p, "HttpOnly"):
			c.HttpOnly = true
		case strings.EqualFold(p, "Secure"):
			c.Secure = true
		}
	}
	return c
}
