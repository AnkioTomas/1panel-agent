package master

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"1panel-agent/internal/protocol"
)

// handleSwitch 校验 Agent 在线后：暂存本机会话 → 预热远端自动登录 → 写入远端 psession → 进首页。
//
// 旧逻辑只设 mp_node 再跳安全入口：浏览器里仍是本机 psession，1Panel SPA 读错会话，
// 第一次切节点必掉登录页；手动登录后才“碰巧”好了。
func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	sess, ok := s.reg.Get(id)
	if !ok {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}

	stashLocalPanelCookies(w, r)

	remoteCookies, err := s.warmAgentSession(sess)
	if err != nil {
		http.Error(w, "agent auto-login failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	writePanelCookies(w, remoteCookies)

	http.SetCookie(w, &http.Cookie{
		Name:     "mp_node",
		Value:    id,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	// 进首页，不进安全入口页：EntranceCode 由 Agent 侧请求头注入。
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLocal 恢复本机会话，清掉远端/暂存 Cookie，切回本机 1Panel。
func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request) {
	expirePanelCookies(w)
	restoreLocalPanelCookies(w, r)

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
