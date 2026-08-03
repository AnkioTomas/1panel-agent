package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"

	"github.com/coder/websocket"
	"github.com/xtaci/smux"
)

type Client struct {
	Cfg *config.Agent
}

func Run(cfg *config.Agent) error {
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

func (c *Client) connectOnce() error {
	if c.Cfg.Master == "" || c.Cfg.Token == "" {
		return fmt.Errorf("master/token not configured; run agent register first")
	}
	AutofillPanel(c.Cfg)
	if c.Cfg.PanelURL == "" {
		c.Cfg.PanelURL = config.DefaultPanelURL
	}

	wsURL := url.URL{
		Scheme:   "ws",
		Host:     c.Cfg.Master,
		Path:     "/agent/ws",
		RawQuery: "token=" + url.QueryEscape(c.Cfg.Token),
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
	default:
		c.handleHTTP(stream, meta, body)
	}
}

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
	panel.InjectAuth(req.Header, c.Cfg.PanelKey)

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
	if err := protocol.WriteJSON(stream, respMeta); err != nil {
		return
	}
	_ = protocol.CopyChunks(stream, resp.Body)
}

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
	panel.InjectAuth(req.Header, c.Cfg.PanelKey)

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
