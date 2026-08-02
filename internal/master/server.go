package master

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"

	"github.com/coder/websocket"
	"github.com/xtaci/smux"
)

type Server struct {
	Listen     string
	Token      string
	PublicHost string
	Entrance   string
	PanelUser  string
	PanelPass  string
	LocalPanel string // http://127.0.0.1:internal
	reg        *Registry
	localProxy *httputil.ReverseProxy
}

type Options struct {
	Listen     string
	Token      string
	PublicHost string
	Entrance   string
	PanelUser  string
	PanelPass  string
	LocalPanel string
	Takeover   bool
	DBPath     string
}

func New(opts Options) (*Server, error) {
	state, err := config.LoadMasterOrEmpty()
	if err != nil {
		return nil, err
	}
	if opts.Token != "" {
		state.Token = opts.Token
	}
	if opts.PanelUser != "" {
		state.PanelUser = opts.PanelUser
	}
	if opts.PanelPass != "" {
		state.PanelPassword = opts.PanelPass
	}
	if opts.PublicHost != "" {
		state.PublicHost = opts.PublicHost
	}
	if opts.Entrance != "" {
		state.Entrance = opts.Entrance
	}

	listen := opts.Listen
	localPanel := opts.LocalPanel
	entrance := state.Entrance

	if opts.Takeover {
		pub, internal, ent, err := EnsureTakeover(opts.DBPath, state)
		if err != nil {
			return nil, err
		}
		entrance = ent
		if listen == "" {
			listen = fmt.Sprintf(":%d", pub)
		}
		if localPanel == "" {
			localPanel = panel.LocalPanelURL(internal)
		}
		_ = config.SaveMaster(state)
	}

	if state.Token == "" {
		return nil, fmt.Errorf("token required")
	}
	if listen == "" {
		listen = ":8080"
	}
	if localPanel == "" && state.InternalPort > 0 {
		localPanel = panel.LocalPanelURL(state.InternalPort)
	}

	s := &Server{
		Listen:     listen,
		Token:      state.Token,
		PublicHost: state.PublicHost,
		Entrance:   entrance,
		PanelUser:  state.PanelUser,
		PanelPass:  state.PanelPassword,
		LocalPanel: localPanel,
		reg:        NewRegistry(),
	}
	if s.LocalPanel != "" {
		u, err := url.Parse(s.LocalPanel)
		if err != nil {
			return nil, err
		}
		s.localProxy = httputil.NewSingleHostReverseProxy(u)
		s.localProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "local 1Panel unavailable: "+err.Error(), http.StatusBadGateway)
		}
		s.wrapLocalProxy()
	}
	if err := InjectSidebarMenu(opts.DBPath); err != nil {
		log.Printf("warn: inject sidebar menu: %v", err)
	}
	return s, nil
}

func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", s.handleAgentWS)
	mux.HandleFunc("/__mp/", s.handleMP)
	mux.HandleFunc("/n/", s.handleProxy)
	mux.HandleFunc("/", s.handleRoot)

	srv := &http.Server{
		Addr:              s.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("master listening on %s (local panel=%s entrance=%s)", s.Listen, s.LocalPanel, s.Entrance)
	return srv.ListenAndServe()
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__mp" {
		http.Redirect(w, r, "/__mp/", http.StatusFound)
		return
	}
	// Active remote node (cookie): root-path tunnel so 1Panel absolute /assets work.
	if c, err := r.Cookie("mp_node"); err == nil && c.Value != "" {
		if sess, ok := s.reg.Get(c.Value); ok {
			targetPath := r.URL.Path
			if r.URL.RawQuery != "" {
				targetPath += "?" + r.URL.RawQuery
			}
			if isWebSocket(r) {
				s.proxyWebSocket(w, r, sess, targetPath, "")
				return
			}
			s.proxyHTTP(w, r, sess, targetPath, "")
			return
		}
	}
	if s.localProxy != nil {
		s.localProxy.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/__mp/", http.StatusFound)
		return
	}
	http.NotFound(w, r)
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
	if prefix == "" {
		// Root-path remote node: keep local psession intact, use mp_r_* for agent.
		applyRemoteRequestCookies(headers, r)
	}
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

	reqBody := io.Reader(http.NoBody)
	if r.Body != nil {
		reqBody = r.Body
		defer r.Body.Close()
	}
	if err := protocol.CopyChunks(stream, reqBody); err != nil {
		http.Error(w, "tunnel write body: "+err.Error(), http.StatusBadGateway)
		return
	}

	respMeta, err := protocol.ReadResponseMeta(stream)
	if err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return
	}

	respBody, err := io.ReadAll(protocol.NewChunkReader(stream))
	if err != nil {
		http.Error(w, "tunnel read body: "+err.Error(), http.StatusBadGateway)
		return
	}
	ct := ""
	for k, vals := range respMeta.Headers {
		if http.CanonicalHeaderKey(k) == "Content-Type" && len(vals) > 0 {
			ct = vals[0]
			break
		}
	}
	if prefix == "" && respMeta.Status == http.StatusOK && strings.Contains(ct, "text/html") {
		respBody = injectHookHTML(respBody)
	}
	if prefix == "" {
		rewriteSetCookieToRemoteNamespace(respMeta.Headers)
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
	h.Set("Content-Length", fmt.Sprintf("%d", len(respBody)))
	w.WriteHeader(respMeta.Status)
	_, _ = w.Write(respBody)
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
	if prefix == "" {
		return loc
	}
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
	if prefix == "" {
		return v
	}
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
	if prefix == "" {
		applyRemoteRequestCookies(headers, r)
	}
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
	if err := protocol.CopyChunks(stream, http.NoBody); err != nil {
		http.Error(w, "tunnel write body: "+err.Error(), http.StatusBadGateway)
		return
	}

	respMeta, err := protocol.ReadResponseMeta(stream)
	if err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return
	}
	if prefix == "" {
		rewriteSetCookieToRemoteNamespace(respMeta.Headers)
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

// AdvertiseHost returns host:port for agent register command.
func (s *Server) AdvertiseHost(r *http.Request) string {
	if s.PublicHost != "" {
		if strings.Contains(s.PublicHost, ":") {
			return s.PublicHost
		}
		_, port, _ := net.SplitHostPort(s.Listen)
		if port == "" {
			if strings.HasPrefix(s.Listen, ":") {
				port = strings.TrimPrefix(s.Listen, ":")
			} else {
				port = "80"
			}
		}
		return s.PublicHost + ":" + port
	}
	host := r.Host
	if host == "" {
		host = s.Listen
	}
	return host
}
