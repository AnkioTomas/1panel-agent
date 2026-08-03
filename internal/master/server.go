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
	"sync"
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
	tokenMu    sync.RWMutex
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
}

func New(opts Options) (*Server, error) {
	state, err := config.LoadMasterOrEmpty()
	if err != nil {
		return nil, err
	}
	dirty := false
	if opts.Token != "" {
		state.Token = opts.Token
		dirty = true
	}
	if opts.PanelPass != "" {
		state.PanelPassword = opts.PanelPass
		dirty = true
	}
	if opts.PublicHost != "" {
		state.PublicHost = opts.PublicHost
		dirty = true
	}
	if opts.Entrance != "" {
		state.Entrance = opts.Entrance
		dirty = true
	}
	if opts.PanelUser != "" {
		state.PanelUser = opts.PanelUser
		dirty = true
	}

	listen := opts.Listen
	localPanel := opts.LocalPanel
	entrance := state.Entrance

	// Sync username/entrance from 1panel CLI (never open core.db).
	if st, err := panel.ReadSettings(); err == nil {
		if opts.PanelUser == "" && st.UserName != "" && state.PanelUser != st.UserName {
			state.PanelUser = st.UserName
			dirty = true
		}
		if opts.Entrance == "" && st.SecurityEntrance != "" {
			entrance = st.SecurityEntrance
			if state.Entrance != entrance {
				state.Entrance = entrance
				dirty = true
			}
		}
	}

	if opts.Takeover {
		pub, internal, ent, err := EnsureTakeover(state)
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
		dirty = true
	}

	if state.Token == "" {
		tok, err := config.GenerateToken()
		if err != nil {
			return nil, err
		}
		state.Token = tok
		dirty = true
		log.Printf("generated install token (rotate anytime in /__mp/)")
	}
	if dirty {
		_ = config.SaveMaster(state)
	}
	if state.PanelPassword == "" {
		log.Printf("warn: panel password not set — run: 1pm master set --panel-pass PASS (needed for node switch login)")
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
	return s, nil
}

func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", s.handleAgentWS)
	mux.HandleFunc("/agent.sh", s.handleAgentScript)
	mux.HandleFunc("/agent.bin", s.handleAgentBinary)
	mux.HandleFunc("/__mp/", s.handleMP)
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
				s.proxyWebSocket(w, r, sess, targetPath)
				return
			}
			s.proxyHTTP(w, r, sess, targetPath)
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
	if !s.tokenOK(token) {
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

	session, err := smux.Server(netConn, protocol.SmuxConfig())
	if err != nil {
		log.Printf("smux server: %v", err)
		return
	}

	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}
	info := AgentInfo{
		ID:           reg.ID,
		Hostname:     reg.Hostname,
		PanelURL:     reg.PanelURL,
		RemoteIP:     remoteIP,
		PanelVersion: reg.PanelVersion,
	}
	s.reg.Put(&Session{Info: info, Mux: session})
	log.Printf("agent online: %s (%s) from %s version=%s", info.Hostname, info.ID, remoteIP, info.PanelVersion)

	<-session.CloseChan()
	s.reg.Remove(info.ID, session)
	log.Printf("agent offline: %s (%s)", info.Hostname, info.ID)
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (s *Server) proxyHTTP(w http.ResponseWriter, r *http.Request, sess *Session, targetPath string) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		http.Error(w, "tunnel open failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	headers := protocol.HeaderFromHTTP(r.Header)
	delete(headers, "Host")
	// Force identity encoding so we can inject HTML into the tunnel response body.
	delete(headers, "Accept-Encoding")
	for k := range headers {
		if strings.EqualFold(k, "Accept-Encoding") {
			delete(headers, k)
		}
	}
	// Keep local psession intact; use mp_r_* cookies for the agent panel.
	applyRemoteRequestCookies(headers, r)
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

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
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
	respBody = maybeGunzip(respBody, respMeta.Headers)
	dropHopHeaders(respMeta.Headers)
	if respMeta.Status == http.StatusOK && strings.Contains(strings.ToLower(ct), "text/html") {
		respBody = s.injectHookHTML(respBody, s.DeviceIP())
	}
	rewriteSetCookieToRemoteNamespace(respMeta.Headers)

	h := w.Header()
	for k, vals := range respMeta.Headers {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Length" || ck == "Transfer-Encoding" || ck == "Content-Encoding" {
			continue
		}
		for _, v := range vals {
			h.Add(k, v)
		}
	}
	h.Set("Content-Length", fmt.Sprintf("%d", len(respBody)))
	w.WriteHeader(respMeta.Status)
	_, _ = w.Write(respBody)
}

func (s *Server) proxyWebSocket(w http.ResponseWriter, r *http.Request, sess *Session, targetPath string) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		http.Error(w, "tunnel open failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	headers := protocol.HeaderFromHTTP(r.Header)
	delete(headers, "Host")
	applyRemoteRequestCookies(headers, r)
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

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return
	}
	rewriteSetCookieToRemoteNamespace(respMeta.Headers)
	if respMeta.Status != http.StatusSwitchingProtocols {
		h := w.Header()
		for k, vals := range respMeta.Headers {
			ck := http.CanonicalHeaderKey(k)
			if ck == "Content-Length" || ck == "Transfer-Encoding" {
				continue
			}
			for _, v := range vals {
				h.Add(k, v)
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

// listenPort returns the port Master is bound to.
func (s *Server) listenPort() string {
	_, port, _ := net.SplitHostPort(s.Listen)
	if port != "" {
		return port
	}
	if strings.HasPrefix(s.Listen, ":") {
		return strings.TrimPrefix(s.Listen, ":")
	}
	return "80"
}

// AdvertiseHost returns host:port for the agent install command.
// Prefer the Host the browser actually used; PublicHost is an optional override (NAT).
func (s *Server) AdvertiseHost(r *http.Request) string {
	if s.PublicHost != "" {
		if strings.Contains(s.PublicHost, ":") {
			return s.PublicHost
		}
		return s.PublicHost + ":" + s.listenPort()
	}
	if r != nil && r.Host != "" {
		return r.Host
	}
	if ip := DetectLANIP(); ip != "" {
		return ip + ":" + s.listenPort()
	}
	return s.Listen
}
