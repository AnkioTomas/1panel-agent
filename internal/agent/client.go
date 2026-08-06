// Package agent 实现 1pm Agent：注册到 Master、维持隧道并代理本机 1Panel。
package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"
	"1panel-agent/internal/role"

	"github.com/coder/websocket"
	"github.com/xtaci/smux"
)

// Client 封装了 Agent 与 Master 通信并代理本地 1Panel 的客户端结构。
type Client struct {
	Cfg *config.Agent

	sessMu      sync.Mutex
	sessCookies []*http.Cookie
	sessUntil   time.Time

	muxMu sync.Mutex
	mux   *smux.Session

	panelHTTP *http.Client
}

// Run 启动 Agent 客户端逻辑，维持与 Master 的长连接并处理自动重连。
func Run(cfg *config.Agent) error {
	if err := role.RefuseAgentIfMaster(); err != nil {
		return err
	}
	c := &Client{Cfg: cfg}
	if err := EnsureLocalPanelHardening(cfg); err != nil {
		log.Printf("local panel hardening: %v", err)
	}
	backoff := time.Second
	for {
		err := c.connectOnce()
		if err != nil {
			log.Printf("agent disconnected: %v; retry in %s", err, backoff)
		} else {
			log.Printf("agent session closed; retry in %s", backoff)
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// connectOnce 完成一次鉴权连接、注册与 smux Accept 循环；断开则返回错误。
func (c *Client) connectOnce() error {
	if c.Cfg.Master == "" || c.Cfg.Token == "" {
		return fmt.Errorf("master/token not configured; run agent install first")
	}
	if err := AutofillPanel(c.Cfg); err != nil {
		return err
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign(c.Cfg.Token, ts)

	scheme := "ws"
	if c.Cfg.MasterTLS {
		scheme = "wss"
	}
	wsURL := url.URL{
		Scheme:   scheme,
		Host:     c.Cfg.Master,
		Path:     "/agent/ws",
		RawQuery: fmt.Sprintf("timestamp=%s&sign=%s", ts, sign),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialOpts := &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled}
	if c.Cfg.MasterTLS {
		dialOpts.HTTPClient = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 面板自签常见
			},
		}
	}
	conn, _, err := websocket.Dial(ctx, wsURL.String(), dialOpts)
	if err != nil {
		return fmt.Errorf("%s dial: %w", scheme, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	netConn := websocket.NetConn(context.Background(), conn, websocket.MessageBinary)

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = c.Cfg.ID
	}
	reg := protocol.Register{
		ID:           c.Cfg.ID,
		Hostname:     hostname,
		Name:         c.Cfg.Name,
		Group:        c.Cfg.Group,
		PanelURL:     c.Cfg.PanelURL,
		PanelVersion: panel.ReadSystemVersion(),
		AgentVersion: buildinfo.Version,
	}
	if err := protocol.WriteJSON(netConn, reg); err != nil {
		return fmt.Errorf("register write: %w", err)
	}

	var ack protocol.RegisterOK
	if err := protocol.ReadJSON(netConn, &ack); err != nil {
		return fmt.Errorf("register ack: %w", err)
	}
	if !ack.OK {
		return fmt.Errorf("register rejected: %s", ack.Error)
	}

	session, err := smux.Client(netConn, protocol.SmuxConfig())
	if err != nil {
		return fmt.Errorf("smux client: %w", err)
	}
	defer session.Close()
	c.setSession(session)
	defer c.setSession(nil)

	log.Printf("connected to master %s as %s", c.Cfg.Master, c.Cfg.ID)
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			return err
		}
		go c.handleStream(stream)
	}
}

// handleStream 读取流元数据并分发给 HTTP 或 WebSocket 处理。
func (c *Client) handleStream(stream *smux.Stream) {
	defer stream.Close()

	meta, err := protocol.ReadRequestMeta(stream)
	if err != nil {
		log.Printf("read request meta: %v", err)
		return
	}
	body := protocol.NewChunkReader(stream)

	switch meta.Type {
	case protocol.StreamTypeWS:
		c.handleWS(stream, meta, body)
	case protocol.StreamTypeStats:
		c.handleStats(stream, body)
	case protocol.StreamTypeUpdate:
		c.handleUpdate(stream, body)
	case protocol.StreamTypePanelUpgrade:
		c.handlePanelUpgrade(stream, body)
	case protocol.StreamTypePanelControl:
		c.handlePanelControl(stream, body)
	default:
		c.handleHTTP(stream, meta, body)
	}
}

func (c *Client) setSession(sess *smux.Session) {
	c.muxMu.Lock()
	c.mux = sess
	c.muxMu.Unlock()
}

func (c *Client) closeSession() {
	c.muxMu.Lock()
	sess := c.mux
	c.mux = nil
	c.muxMu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
}

// handleStats 响应 Master 的主机状态查询。
func (c *Client) handleStats(stream *smux.Stream, body io.Reader) {
	_, _ = io.Copy(io.Discard, body)
	c.writeJSON(stream, collectHostStats())
}

// getSessionCookies 在配置了加密密码时登录本机 1Panel，返回会话 Cookie（带短缓存）。
func (c *Client) getSessionCookies() []*http.Cookie {
	if c.Cfg.PanelUser == "" {
		return nil
	}
	pass, err := c.Cfg.PanelPasswordPlain()
	if err != nil || pass == "" {
		return nil
	}

	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	if time.Now().Before(c.sessUntil) && len(c.sessCookies) > 0 {
		return c.sessCookies
	}

	res, err := panel.Login(c.Cfg.PanelURL, c.Cfg.PanelEntrance, c.Cfg.PanelUser, pass)
	if err != nil {
		log.Printf("agent auto-login failed: %v", err)
		c.sessCookies = nil
		c.sessUntil = time.Time{}
		return nil
	}
	c.sessCookies = res.Cookies
	c.sessUntil = time.Now().Add(10 * time.Minute)
	log.Printf("agent auto-login ok (cookies=%d entrance=%q)", len(res.Cookies), c.Cfg.PanelEntrance)
	return c.sessCookies
}

func (c *Client) clearSession() {
	c.sessMu.Lock()
	c.sessCookies = nil
	c.sessUntil = time.Time{}
	c.sessMu.Unlock()
}

// handleHTTP 将隧道 HTTP 请求转发到本机 1Panel，并把响应写回流。
// 静态资源边读边写；HTML/JSON/401 仍整包（会话探测 + Master 注入需要）。
func (c *Client) handleHTTP(stream *smux.Stream, meta *protocol.RequestMeta, body io.Reader) {
	panelURL, err := url.Parse(c.Cfg.PanelURL)
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, "bad panel_url: "+err.Error())
		return
	}

	path := meta.Path
	if path == "" {
		path = "/"
	}
	ref, err := url.Parse(path)
	if err != nil {
		c.writeErr(stream, http.StatusBadRequest, "bad path")
		return
	}
	if ref.Path == "" {
		ref.Path = "/"
	} else if !strings.HasPrefix(ref.Path, "/") {
		ref.Path = "/" + ref.Path
	}
	target := panelURL.ResolveReference(ref)

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, "read body: "+err.Error())
		return
	}

	for attempt := range 2 {
		injected, resp, err := c.roundTripPanel(meta, target, panelURL.Host, bodyBytes)
		if err != nil {
			c.writeErr(stream, http.StatusBadGateway, err.Error())
			return
		}

		status := resp.StatusCode
		hdrs := protocol.HeaderFromHTTP(resp.Header)
		ct := resp.Header.Get("Content-Type")

		if protocol.CanStreamHTTP(status, ct) {
			if protocol.IsCacheableAsset(ct) {
				// 静态资源绝不能带 Set-Cookie：浏览器会拒写/跳过 disk cache，
				// 即使上游已有 Cache-Control: immutable。会话 Cookie 只在 HTML/API 回写。
				protocol.DeleteHeader(hdrs, "Set-Cookie")
			} else {
				appendSessionSetCookies(hdrs, injected)
			}
			respMeta := &protocol.ResponseMeta{Status: status, Headers: hdrs}
			if err := protocol.WriteJSON(stream, respMeta); err != nil {
				_ = resp.Body.Close()
				return
			}
			_ = protocol.CopyChunks(stream, resp.Body)
			_ = resp.Body.Close()
			return
		}

		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			c.writeErr(stream, http.StatusBadGateway, err.Error())
			return
		}

		if attempt == 0 && panelUnauthenticated(status, protocol.MaybeGunzip(raw, hdrs)) {
			c.clearSession()
			continue
		}

		appendSessionSetCookies(hdrs, injected)
		respMeta := &protocol.ResponseMeta{Status: status, Headers: hdrs}
		if err := protocol.WriteJSON(stream, respMeta); err != nil {
			return
		}
		_ = protocol.CopyChunks(stream, bytes.NewReader(raw))
		return
	}
}

func (c *Client) roundTripPanel(meta *protocol.RequestMeta, target *url.URL, host string, body []byte) ([]*http.Cookie, *http.Response, error) {
	req, err := http.NewRequest(meta.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	protocol.ApplyHeader(req.Header, meta.Headers)
	req.Host = host
	panel.ApplyEntrance(req.Header, c.Cfg.PanelEntrance)

	injected := c.getSessionCookies()
	applyAgentSession(req, injected)
	panel.AlignCSRF(req.Header)

	resp, err := c.panelClient().Do(req)
	if err != nil {
		return injected, nil, err
	}
	return injected, resp, nil
}

// handleWS 将隧道 WebSocket 升级请求转发到本机 1Panel 并双向拷贝帧。
func (c *Client) handleWS(stream *smux.Stream, meta *protocol.RequestMeta, body io.Reader) {
	// Drain empty/chunked upgrade body.
	_, _ = io.Copy(io.Discard, body)

	panelURL, err := url.Parse(c.Cfg.PanelURL)
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, "bad panel_url")
		return
	}
	ref, err := url.Parse(meta.Path)
	if err != nil {
		c.writeErr(stream, http.StatusBadRequest, "bad path")
		return
	}
	if !strings.HasPrefix(ref.Path, "/") {
		ref.Path = "/" + ref.Path
	}
	target := panelURL.ResolveReference(ref)

	host := panelURL.Host
	network := "tcp"
	dialAddr := host
	useTLS := panelURL.Scheme == "https"

	var conn net.Conn
	if useTLS {
		conn, err = tls.Dial(network, dialAddr, &tls.Config{InsecureSkipVerify: true, ServerName: panelURL.Hostname()})
	} else {
		conn, err = net.DialTimeout(network, dialAddr, 10*time.Second)
	}
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	req := &http.Request{
		Method: http.MethodGet,
		URL:    target,
		Host:   host,
		Header: make(http.Header),
		Proto:  "HTTP/1.1",
	}
	protocol.ApplyHeader(req.Header, meta.Headers)
	req.Header.Set("Host", host)
	panel.ApplyEntrance(req.Header, c.Cfg.PanelEntrance)

	applyAgentSession(req, c.getSessionCookies())
	panel.AlignCSRF(req.Header)

	if err := req.Write(conn); err != nil {
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}

	respMeta := &protocol.ResponseMeta{
		Status:  resp.StatusCode,
		Headers: protocol.HeaderFromHTTP(resp.Header),
	}
	if err := protocol.WriteJSON(stream, respMeta); err != nil {
		return
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = protocol.CopyChunks(stream, resp.Body)
		_ = resp.Body.Close()
		return
	}
	_ = resp.Body.Close()

	// Buffered bytes after response belong to the WS stream.
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(stream, br)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(conn, stream)
		errc <- err
	}()
	<-errc
}

// writeErr 向隧道写一条错误响应元数据与纯文本 body。
func (c *Client) writeErr(stream *smux.Stream, status int, msg string) {
	meta := &protocol.ResponseMeta{
		Status: status,
		Headers: map[string][]string{
			"Content-Type": {"text/plain; charset=utf-8"},
		},
	}
	if err := protocol.WriteJSON(stream, meta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, strings.NewReader(msg))
}

// writeJSON 向隧道写一条 200 JSON 响应。
func (c *Client) writeJSON(stream *smux.Stream, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		c.writeErr(stream, http.StatusInternalServerError, err.Error())
		return
	}
	meta := &protocol.ResponseMeta{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	if err := protocol.WriteJSON(stream, meta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, bytes.NewReader(raw))
}

func (c *Client) panelClient() *http.Client {
	if c.panelHTTP != nil {
		return c.panelHTTP
	}
	c.panelHTTP = &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:               nil,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // local panel often self-signed
			DisableCompression:  true,
			MaxIdleConnsPerHost: 8,
		},
	}
	return c.panelHTTP
}

// applyAgentSession 去掉请求里的面板会话 Cookie，再注入 Agent 自持会话。
func applyAgentSession(req *http.Request, cookies []*http.Cookie) {
	kept := nonPanelCookieHeader(req.Header.Get("Cookie"))
	req.Header.Del("Cookie")
	if kept != "" {
		req.Header.Set("Cookie", kept)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
}

func nonPanelCookieHeader(header string) string {
	var parts []string
	for part := range strings.SplitSeq(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if !ok || panel.IsSessionCookie(name) {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func panelUnauthenticated(status int, body []byte) bool {
	if status == http.StatusUnauthorized {
		return true
	}
	var ar struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &ar); err != nil {
		return false
	}
	return ar.Code == 401
}

// appendSessionSetCookies 把自动登录得到的会话 Cookie 写回响应，供浏览器保存。
func appendSessionSetCookies(headers map[string][]string, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	for _, c := range cookies {
		if !panel.IsSessionCookie(c.Name) {
			continue
		}
		sc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     "/",
			HttpOnly: c.HttpOnly,
			SameSite: c.SameSite,
			Secure:   c.Secure,
		}
		if c.MaxAge > 0 {
			sc.MaxAge = c.MaxAge
		}
		headers["Set-Cookie"] = append(headers["Set-Cookie"], sc.String())
	}
}
