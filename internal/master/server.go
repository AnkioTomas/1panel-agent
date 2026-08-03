// Package master 实现 1pm Master：接管本机 1Panel 公网端口、管理 Agent 隧道与管理页。
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
	"1panel-agent/internal/role"

	"github.com/coder/websocket"
	"github.com/xtaci/smux"
)

// Server 是 Master HTTP 服务的运行时状态。
type Server struct {
	Listen        string // 对外监听地址，通常为 :原1Panel端口
	Token         string // Agent 安装/注册 HMAC 密钥
	PublicHost    string // 可选 NAT 对外 host[:port]；空则用请求 Host
	Entrance      string // 1Panel 安全入口路径段
	PanelUser     string // 本机 1Panel 用户名（展示用）
	LocalPanel    string // 内部避让地址，形如 http://127.0.0.1:<internal>
	sessionSecret string // 内存 Web Session Secret（绝不上盘）
	reg           *Registry
	localProxy    *httputil.ReverseProxy
	tokenMu       sync.RWMutex

	// 切到子节点时暂存的本机面板会话；不落盘。Agent Cookie 不在此列。
	localSessMu      sync.Mutex
	localSessCookies []*http.Cookie
}

// New 加载 Master 状态、接管 1Panel 端口并构造 Server。
func New() (*Server, error) {
	if err := role.RefuseMasterIfAgent(); err != nil {
		return nil, err
	}
	state, err := config.LoadMasterOrEmpty()
	if err != nil {
		return nil, err
	}

	pub, internal, entrance, panelUser, err := EnsureTakeover(state)
	if err != nil {
		return nil, fmt.Errorf("takeover 1Panel port: %w", err)
	}
	listen := fmt.Sprintf(":%d", pub)
	localPanel := panel.LocalPanelURL(internal)

	if state.Token == "" {
		tok, err := config.GenerateToken()
		if err != nil {
			return nil, err
		}
		state.Token = tok
		log.Printf("generated install token (rotate anytime in /__mp/)")
		_ = config.SaveMaster(state)
	}

	s := &Server{
		Listen:     listen,
		Token:      state.Token,
		PublicHost: state.PublicHost,
		Entrance:   entrance,
		PanelUser:  panelUser,
		LocalPanel: localPanel,
		reg:        NewRegistry(),
	}
	if s.LocalPanel != "" {
		u, err := url.Parse(s.LocalPanel)
		if err != nil {
			return nil, fmt.Errorf("parse local_panel url: %w", err)
		}
		s.localProxy = httputil.NewSingleHostReverseProxy(u)
		s.localProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "local 1Panel unavailable: "+err.Error(), http.StatusBadGateway)
		}
		s.wrapLocalProxy()
	}
	return s, nil
}

// Run 注册路由并阻塞监听；返回 ListenAndServe 的错误。
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

// handleRoot 处理根路径：有 mp_node 则走 Agent 隧道，否则反代本机 1Panel。
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__mp" {
		http.Redirect(w, r, "/__mp/", http.StatusFound)
		return
	}
	// Active remote node (cookie): root-path tunnel so 1Panel absolute /assets work.
	if c, err := r.Cookie("mp_node"); err == nil && c.Value != "" {
		// 登录页必须落在主节点：清 mp_node 后进安全入口。
		if isLoginPagePath(r.URL.Path) {
			s.redirectToMasterLogin(w, r, "")
			return
		}
		if sess, ok := s.reg.Get(c.Value); ok {
			targetPath := r.URL.RequestURI()
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				s.proxyWebSocket(w, r, sess, targetPath)
				return
			}
			s.proxyHTTP(w, r, sess, targetPath)
			return
		}
		// Agent 已离线：丢掉过期节点选择，回主节点。
		clearNodeCookie(w)
	}
	if s.localProxy != nil {
		s.localProxy.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

// handleAgentWS 校验签名后接受 Agent WebSocket，完成注册并挂入 smux Registry。
func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	ts := r.URL.Query().Get("timestamp")
	sign := r.URL.Query().Get("sign")
	if !s.VerifyToken(ts, sign) {
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
		Name:         reg.Name,
		Group:        reg.Group,
		PanelURL:     reg.PanelURL,
		RemoteIP:     remoteIP,
		PanelVersion: reg.PanelVersion,
		AgentVersion: reg.AgentVersion,
	}
	s.reg.Put(&Session{Info: info, Mux: session})
	log.Printf("agent online: %s (%s) group=%q from %s agent=%s panel=%s", info.DisplayName(), info.ID, info.Group, remoteIP, info.AgentVersion, info.PanelVersion)

	<-session.CloseChan()
	s.reg.Remove(info.ID, session)
	log.Printf("agent offline: %s (%s)", info.Hostname, info.ID)
}

// proxyHTTP 经 smux 流代理 HTTP，必要时解压并注入侧边栏 Hook。
func (s *Server) proxyHTTP(w http.ResponseWriter, r *http.Request, sess *Session, targetPath string) {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		http.Error(w, "tunnel open failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	headers := protocol.HeaderFromHTTP(r.Header)
	delete(headers, "Host")
	// http.Header keys are canonical; force identity encoding for HTML injection.
	delete(headers, "Accept-Encoding")
	// 面板会话由 Agent 自持；控制 Cookie / 本机 psession 不进隧道。
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

	// 子节点把浏览器踢去登录页时，改走主节点登录。
	if locationIsLoginRedirect(respMeta.Headers) {
		s.redirectToMasterLogin(w, r, "")
		return
	}

	if respMeta.Status == http.StatusOK && strings.Contains(strings.ToLower(ct), "text/html") {
		respBody = s.injectHookHTML(respBody, s.displayHost(r))
	}
	normalizeRemoteSetCookies(respMeta.Headers)

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

// proxyWebSocket 经 smux 流完成浏览器与远端 1Panel 的 WebSocket 双向转发。
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
	normalizeRemoteSetCookies(respMeta.Headers)
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

// listenPort 从 Listen 地址解析对外端口；失败时回退 "80"。
func (s *Server) listenPort() string {
	_, port, _ := net.SplitHostPort(s.Listen)
	if port != "" {
		return port
	}
	if after, ok := strings.CutPrefix(s.Listen, ":"); ok {
		return after
	}
	return "80"
}

// AdvertiseHost 返回安装命令用的 host:port。
// 优先 PublicHost（NAT），否则用浏览器 Host；不再猜测网卡 IP。
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
	return s.Listen
}

// displayHost 返回 UI/注入脚本用的主机名：PublicHost 或请求 Host，无则空。
func (s *Server) displayHost(r *http.Request) string {
	if s.PublicHost != "" {
		host := s.PublicHost
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host != "" && host != "0.0.0.0" && host != "::" {
			return host
		}
	}
	if r != nil && r.Host != "" {
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			return h
		}
		return r.Host
	}
	return ""
}
