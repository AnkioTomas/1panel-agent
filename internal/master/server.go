package master

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"1panel-agent/internal/protocol"

	"github.com/coder/websocket"
	"github.com/xtaci/smux"
)

type Server struct {
	Listen string
	Token  string
	reg    *Registry
}

func New(listen, token string) *Server {
	return &Server{
		Listen: listen,
		Token:  token,
		reg:    NewRegistry(),
	}
}

func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/agent/ws", s.handleAgentWS)
	mux.HandleFunc("/n/", s.handleProxy)

	srv := &http.Server{
		Addr:              s.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("master listening on %s", s.Listen)
	return srv.ListenAndServe()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	agents := s.reg.List()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, agents)
}

func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" || token != s.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		log.Printf("agent ws accept: %v", err)
		return
	}

	ctx := context.Background()
	netConn := websocket.NetConn(ctx, conn, websocket.MessageBinary)

	var reg protocol.Register
	if err := protocol.ReadJSON(netConn, &reg); err != nil {
		log.Printf("agent register read: %v", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "bad register")
		return
	}
	if reg.ID == "" || reg.Hostname == "" {
		_ = protocol.WriteJSON(netConn, protocol.RegisterOK{OK: false, Error: "id and hostname required"})
		_ = conn.Close(websocket.StatusPolicyViolation, "bad register")
		return
	}

	if err := protocol.WriteJSON(netConn, protocol.RegisterOK{OK: true}); err != nil {
		log.Printf("agent register ack: %v", err)
		_ = conn.Close(websocket.StatusInternalError, "ack failed")
		return
	}

	session, err := smux.Server(netConn, smuxConfig())
	if err != nil {
		log.Printf("smux server: %v", err)
		return
	}

	info := AgentInfo{ID: reg.ID, Hostname: reg.Hostname, PanelURL: reg.PanelURL}
	s.reg.Put(&Session{Info: info, Mux: session})
	log.Printf("agent online: %s (%s)", info.Hostname, info.ID)

	<-session.CloseChan()
	s.reg.Remove(info.ID, session)
	log.Printf("agent offline: %s (%s)", info.Hostname, info.ID)
}

func smuxConfig() *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.KeepAliveTimeout = 60 * time.Second
	return cfg
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/n/")
	if rest == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	id, path, found := strings.Cut(rest, "/")
	if id == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	if !found {
		http.Redirect(w, r, "/n/"+id+"/", http.StatusFound)
		return
	}
	prefix := "/n/" + id
	targetPath := "/" + path
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}

	sess, ok := s.reg.Get(id)
	if !ok {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}

	if isWebSocket(r) {
		s.proxyWebSocket(w, r, sess, targetPath, prefix)
		return
	}
	s.proxyHTTP(w, r, sess, targetPath, prefix)
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (s *Server) proxyHTTP(w http.ResponseWriter, r *http.Request, sess *Session, targetPath, prefix string) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		http.Error(w, "tunnel open failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	headers := protocol.HeaderFromHTTP(r.Header)
	delete(headers, "Host")
	meta := &protocol.RequestMeta{
		Type:    protocol.StreamTypeHTTP,
		Method:  r.Method,
		Path:    targetPath,
		Headers: headers,
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		http.Error(w, "tunnel write meta: "+err.Error(), http.StatusBadGateway)
		return
	}

	body := io.Reader(http.NoBody)
	if r.Body != nil {
		body = r.Body
		defer r.Body.Close()
	}
	if err := protocol.CopyChunks(stream, body); err != nil {
		http.Error(w, "tunnel write body: "+err.Error(), http.StatusBadGateway)
		return
	}

	respMeta, err := protocol.ReadResponseMeta(stream)
	if err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return
	}

	h := w.Header()
	for k, vals := range respMeta.Headers {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Length" || ck == "Transfer-Encoding" {
			continue
		}
		for _, v := range vals {
			h.Add(k, rewriteHeaderValue(k, v, prefix))
		}
	}
	w.WriteHeader(respMeta.Status)
	_, _ = io.Copy(w, protocol.NewChunkReader(stream))
}

func rewriteHeaderValue(key, value, prefix string) string {
	switch http.CanonicalHeaderKey(key) {
	case "Location":
		return rewriteLocation(value, prefix)
	case "Set-Cookie":
		return rewriteSetCookie(value, prefix)
	default:
		return value
	}
}

func rewriteLocation(loc, prefix string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	if u.IsAbs() {
		u.Scheme = ""
		u.Host = ""
		rel := u.String()
		if rel == "" {
			rel = "/"
		}
		if !strings.HasPrefix(rel, "/") {
			rel = "/" + rel
		}
		return prefix + rel
	}
	if strings.HasPrefix(loc, "/") {
		return prefix + loc
	}
	return loc
}

func rewriteSetCookie(v, prefix string) string {
	parts := strings.Split(v, ";")
	for i, p := range parts {
		trim := strings.TrimSpace(p)
		if len(trim) >= 5 && strings.EqualFold(trim[:5], "Path=") {
			path := trim[5:]
			if path == "" || path == "/" {
				parts[i] = " Path=" + prefix + "/"
			} else if strings.HasPrefix(path, "/") {
				parts[i] = " Path=" + prefix + path
			}
		} else if i > 0 {
			parts[i] = " " + trim
		}
	}
	return strings.Join(parts, ";")
}

func (s *Server) proxyWebSocket(w http.ResponseWriter, r *http.Request, sess *Session, targetPath, prefix string) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		http.Error(w, "tunnel open failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	headers := protocol.HeaderFromHTTP(r.Header)
	delete(headers, "Host")
	meta := &protocol.RequestMeta{
		Type:    protocol.StreamTypeWS,
		Method:  http.MethodGet,
		Path:    targetPath,
		Headers: headers,
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		http.Error(w, "tunnel write meta: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Empty body for WS upgrade request.
	if err := protocol.CopyChunks(stream, http.NoBody); err != nil {
		http.Error(w, "tunnel write body: "+err.Error(), http.StatusBadGateway)
		return
	}

	respMeta, err := protocol.ReadResponseMeta(stream)
	if err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return
	}
	if respMeta.Status != http.StatusSwitchingProtocols {
		h := w.Header()
		for k, vals := range respMeta.Headers {
			ck := http.CanonicalHeaderKey(k)
			if ck == "Content-Length" || ck == "Transfer-Encoding" {
				continue
			}
			for _, v := range vals {
				h.Add(k, rewriteHeaderValue(k, v, prefix))
			}
		}
		w.WriteHeader(respMeta.Status)
		_, _ = io.Copy(w, protocol.NewChunkReader(stream))
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	for k, vals := range respMeta.Headers {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Length" || ck == "Transfer-Encoding" {
			continue
		}
		for _, v := range vals {
			resp.Header.Add(k, v)
		}
	}
	if err := resp.Write(clientBuf); err != nil {
		return
	}
	if err := clientBuf.Flush(); err != nil {
		return
	}

	// After 101, remaining stream bytes are raw WebSocket frames (not chunked).
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(stream, clientConn)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, stream)
		errc <- err
	}()
	<-errc
}
