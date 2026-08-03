// Package agent 实现 1pm Agent：注册到 Master、维持隧道并代理本机 1Panel。
package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
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
}

// Run 启动 Agent 客户端逻辑，维持与 Master 的长连接并处理自动重连。
func Run(cfg *config.Agent) error {
	if err := role.RefuseAgentIfMaster(); err != nil {
		return err
	}
	c := &Client{Cfg: cfg}
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
	AutofillPanel(c.Cfg)
	if c.Cfg.PanelURL == "" {
		c.Cfg.PanelURL = config.DefaultPanelURL
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign(c.Cfg.Token, ts)

	wsURL := url.URL{
		Scheme:   "ws",
		Host:     c.Cfg.Master,
		Path:     "/agent/ws",
		RawQuery: fmt.Sprintf("timestamp=%s&sign=%s", ts, sign),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
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
	default:
		c.handleHTTP(stream, meta, body)
	}
}

// handleStats 响应 Master 的主机状态查询。
func (c *Client) handleStats(stream *smux.Stream, body io.Reader) {
	_, _ = io.Copy(io.Discard, body)
	st := collectHostStats()
	raw, err := json.Marshal(st)
	if err != nil {
		c.writeErr(stream, http.StatusInternalServerError, err.Error())
		return
	}
	respMeta := &protocol.ResponseMeta{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	if err := protocol.WriteJSON(stream, respMeta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, bytes.NewReader(raw))
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

// applyEntrance 在启用安全入口时注入 EntranceCode 请求头。
func (c *Client) applyEntrance(h http.Header) {
	if c.Cfg.PanelEntrance == "" {
		return
	}
	if h.Get("EntranceCode") != "" {
		return
	}
	h.Set("EntranceCode", base64.StdEncoding.EncodeToString([]byte(c.Cfg.PanelEntrance)))
}

// handleHTTP 将隧道 HTTP 请求转发到本机 1Panel，并把响应写回流。
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

	req, err := http.NewRequest(meta.Method, target.String(), body)
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}
	protocol.ApplyHeader(req.Header, meta.Headers)
	req.Header.Del("Accept-Encoding")
	req.Host = panelURL.Host
	c.applyEntrance(req.Header)

	// 浏览器尚未带远端会话时，注入自动登录 Cookie，并把会话回写给浏览器（经 Master 变成 mp_r_*）。
	var injected []*http.Cookie
	if !cookieHeaderHasName(req.Header.Get("Cookie"), "psession") {
		injected = c.getSessionCookies()
		for _, cookie := range injected {
			req.AddCookie(cookie)
		}
	}
	alignRequestCSRF(req.Header)

	client := &http.Client{
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
	resp, err := client.Do(req)
	if err != nil {
		c.writeErr(stream, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	respMeta := &protocol.ResponseMeta{
		Status:  resp.StatusCode,
		Headers: protocol.HeaderFromHTTP(resp.Header),
	}
	appendSessionSetCookies(respMeta.Headers, injected)
	if err := protocol.WriteJSON(stream, respMeta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, resp.Body)
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
	c.applyEntrance(req.Header)

	if !cookieHeaderHasName(req.Header.Get("Cookie"), "psession") {
		for _, cookie := range c.getSessionCookies() {
			req.AddCookie(cookie)
		}
	}
	alignRequestCSRF(req.Header)

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

// alignRequestCSRF 使 X-CSRF-Token 与 Cookie 中的 pcsrftoken 一致（1Panel 双提交校验）。
func alignRequestCSRF(h http.Header) {
	csrf := cookieValueFromHeader(h.Get("Cookie"), "pcsrftoken")
	if csrf == "" {
		h.Del("X-CSRF-Token")
		return
	}
	h.Set("X-CSRF-Token", csrf)
}

// appendSessionSetCookies 把自动登录得到的会话 Cookie 写回响应，供浏览器存为 mp_r_*。
func appendSessionSetCookies(headers map[string][]string, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	for _, c := range cookies {
		switch strings.ToLower(c.Name) {
		case "psession", "pcsrftoken", "securityentrance", "panel_public_key":
		default:
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

// cookieValueFromHeader 从 Cookie 头解析指定名称的值。
func cookieValueFromHeader(header, name string) string {
	prefix := name + "="
	for part := range strings.SplitSeq(header, ";") {
		part = strings.TrimSpace(part)
		if after, ok := strings.CutPrefix(part, prefix); ok {
			return after
		}
		if len(part) > len(prefix) && strings.EqualFold(part[:len(name)], name) && part[len(name)] == '=' {
			return part[len(name)+1:]
		}
	}
	return ""
}

// cookieHeaderHasName 判断 Cookie 头是否含有指定名称（不会误匹配 mp_r_psession）。
func cookieHeaderHasName(header, name string) bool {
	return cookieValueFromHeader(header, name) != ""
}
