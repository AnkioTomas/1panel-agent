// Package master 实现 1pm Master：接管本机 1Panel 公网端口、管理 Agent 隧道与管理页。
package master

import (
	"context"
	"crypto/tls"
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
	LocalPanel    string // 内部避让地址，形如 http(s)://127.0.0.1:<internal>
	InternalPort  int    // 本机 1Panel 避让端口
	sessionSecret string // 内存 Web Session Secret（绝不上盘）
	reg           *Registry
	localProxy    *httputil.ReverseProxy
	tokenMu       sync.RWMutex

	// 切到子节点时暂存的本机面板会话；不落盘。Agent Cookie 不在此列。
	localSessMu      sync.Mutex
	localSessCookies []*http.Cookie

	updateMu     sync.Mutex
	updateStatus forceUpdateStatus
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
	if syncReleaseSourceFromEnv(state) {
		if err := config.SaveMaster(state); err != nil {
			log.Printf("warn: persist release source: %v", err)
		} else {
			log.Printf("release source: api=%s dl=%s cdn=%s", state.GitHubAPI, state.GitHubDL, state.InstallCDN)
		}
	}

	s := &Server{
		Listen:       listen,
		Token:        state.Token,
		PublicHost:   state.PublicHost,
		Entrance:     entrance,
		PanelUser:    panelUser,
		LocalPanel:   localPanel,
		InternalPort: internal,
		reg:          NewRegistry(),
	}
	s.rebuildLocalProxy()
	return s, nil
}

// rebuildLocalProxy 按当前 LocalPanel（含 https）重建反代。
func (s *Server) rebuildLocalProxy() {
	if s.LocalPanel == "" {
		s.localProxy = nil
		return
	}
	u, err := url.Parse(s.LocalPanel)
	if err != nil {
		log.Printf("warn: parse local_panel url: %v", err)
		return
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "local 1Panel unavailable: "+err.Error(), http.StatusBadGateway)
	}
	if u.Scheme == "https" {
		rp.Transport = &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // 本机面板常自签
			MaxIdleConns:        32,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}
	s.localProxy = rp
	s.wrapLocalProxy()
}

// Run 注册路由并阻塞监听（面板证书可用时自动 HTTPS Mux）。
func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", s.handleAgentWS)
	mux.HandleFunc("/agent.sh", s.handleAgentScript)
	mux.HandleFunc("/agent.bin", s.handleAgentBinary)
	mux.HandleFunc("/__mp/", s.handleMP)
	mux.HandleFunc("/", s.handleRoot)
	return s.listenAndServe(mux)
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
				if s.proxyWebSocket(w, r, sess, targetPath) {
					return
				}
			} else if s.proxyHTTP(w, r, sess, targetPath) {
				return
			}
			// 会话半死（Get 成功但 OpenStream 失败）：清节点并回主节点。
		}
		// Agent 已离线/隧道死：必须恢复本机会话后再 302。
		// 同请求反代本机仍带着远端 Cookie，会直接渲染登录页。
		s.restoreLocalAndRedirect(w, r, "/")
		return
	}
	s.serveLocalPanel(w, r)
}

// serveLocalPanel 反代本机 1Panel；无上游则 404。
func (s *Server) serveLocalPanel(w http.ResponseWriter, r *http.Request) {
	if s.localProxy != nil {
		s.localProxy.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

// dropDeadSession 关闭并移除已无法开流的 Agent 会话。
func (s *Server) dropDeadSession(sess *Session) {
	if sess == nil {
		return
	}
	if sess.Mux != nil {
		_ = sess.Mux.Close()
	}
	s.reg.Remove(sess.Info.ID, sess.Mux)
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
// 静态资源边收边转；HTML/JSON 仍整包处理。
// 返回 false 表示隧道已死，调用方应清 mp_node 并回主节点。
func (s *Server) proxyHTTP(w http.ResponseWriter, r *http.Request, sess *Session, targetPath string) bool {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		log.Printf("tunnel open failed for %s: %v; fallback to local", sess.Info.ID, err)
		s.dropDeadSession(sess)
		return false
	}
	defer stream.Close()

	headers := protocol.HeaderFromHTTP(r.Header)
	delete(headers, "Host")
	// 只谈 gzip：HTML 注入可解压；JS/CSS 压缩体原样回浏览器。
	headers["Accept-Encoding"] = []string{"gzip"}
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
		return true
	}

	reqBody := io.Reader(http.NoBody)
	if r.Body != nil {
		reqBody = r.Body
		defer r.Body.Close()
	}
	if err := protocol.CopyChunks(stream, reqBody); err != nil {
		http.Error(w, "tunnel write body: "+err.Error(), http.StatusBadGateway)
		return true
	}

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return true
	}

	// 子节点把浏览器踢去登录页时，改走主节点登录。
	if locationIsLoginRedirect(respMeta.Headers) {
		_, _ = io.Copy(io.Discard, protocol.NewChunkReader(stream))
		s.redirectToMasterLogin(w, r, "")
		return true
	}

	ct := protocol.HeaderGet(respMeta.Headers, "Content-Type")
	normalizeRemoteSetCookies(respMeta.Headers)
	chunked := protocol.NewChunkReader(stream)

	// 静态资源：拿到 ResponseMeta 立刻回浏览器，边收边转。
	if protocol.CanStreamHTTP(respMeta.Status, ct) {
		h := w.Header()
		for k, vals := range respMeta.Headers {
			ck := http.CanonicalHeaderKey(k)
			if ck == "Transfer-Encoding" {
				continue
			}
			for _, v := range vals {
				h.Add(k, v)
			}
		}
		w.WriteHeader(respMeta.Status)
		_, _ = io.Copy(flushWriter{w: w}, chunked)
		return true
	}

	respBody, err := io.ReadAll(chunked)
	if err != nil {
		http.Error(w, "tunnel read body: "+err.Error(), http.StatusBadGateway)
		return true
	}
	respBody = protocol.MaybeGunzip(respBody, respMeta.Headers)
	dropHopHeaders(respMeta.Headers)

	if respMeta.Status == http.StatusOK && strings.Contains(strings.ToLower(ct), "text/html") {
		respBody = s.injectHookHTML(respBody, s.displayHost(r))
	}

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
	return true
}

// flushWriter 每写一块就 Flush，避免静态资源卡在缓冲里拖高 TTFB。
type flushWriter struct {
	w http.ResponseWriter
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if n > 0 {
		if fl, ok := f.w.(http.Flusher); ok {
			fl.Flush()
		}
	}
	return n, err
}

// proxyWebSocket 经 smux 流完成浏览器与远端 1Panel 的 WebSocket 双向转发。
// 返回 false 表示隧道已死，调用方应清 mp_node 并回主节点。
func (s *Server) proxyWebSocket(w http.ResponseWriter, r *http.Request, sess *Session, targetPath string) bool {
	stream, err := sess.Mux.OpenStream()
	if err != nil {
		log.Printf("ws tunnel open failed for %s: %v; fallback to local", sess.Info.ID, err)
		s.dropDeadSession(sess)
		return false
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
		return true
	}
	if err := protocol.CopyChunks(stream, http.NoBody); err != nil {
		http.Error(w, "tunnel write body: "+err.Error(), http.StatusBadGateway)
		return true
	}

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		http.Error(w, "tunnel read response: "+err.Error(), http.StatusBadGateway)
		return true
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
		return true
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return true
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		return true
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
		return true
	}
	if err := clientBuf.Flush(); err != nil {
		return true
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
	return true
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
