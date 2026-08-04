package master

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"1panel-agent/internal/panel"

	"github.com/soheilhy/cmux"
)

// certStore 持有可热更新的面板 TLS 证书。
type certStore struct {
	cert atomic.Pointer[tls.Certificate]
}

func (c *certStore) get() *tls.Certificate {
	return c.cert.Load()
}

func (c *certStore) set(cert *tls.Certificate) {
	c.cert.Store(cert)
}

func (c *certStore) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := c.get()
	if cert == nil {
		return nil, fmt.Errorf("no tls certificate")
	}
	return cert, nil
}

// startCertReloader 周期性检查面板证书文件，更新 store，并回调 SSL 就绪变化。
func startCertReloader(store *certStore, onChange func(ready bool)) {
	var lastMod time.Time
	var lastReady bool
	tick := func() {
		certFile, keyFile := panel.PanelCertPaths()
		st1, err1 := os.Stat(certFile)
		st2, err2 := os.Stat(keyFile)
		if err1 != nil || err2 != nil {
			if lastReady {
				store.set(nil)
				lastReady = false
				lastMod = time.Time{}
				if onChange != nil {
					onChange(false)
				}
				log.Printf("panel tls cert removed; master HTTP without redirect")
			}
			return
		}
		mod := st1.ModTime()
		if st2.ModTime().After(mod) {
			mod = st2.ModTime()
		}
		if lastReady && !mod.After(lastMod) {
			return
		}
		cert, err := panel.LoadPanelTLS()
		if err != nil {
			if lastReady {
				store.set(nil)
				lastReady = false
				if onChange != nil {
					onChange(false)
				}
			}
			return
		}
		store.set(cert)
		wasReady := lastReady
		lastMod = mod
		lastReady = true
		if !wasReady {
			log.Printf("panel tls cert loaded from %s", panel.SecretDir())
		}
		if onChange != nil {
			onChange(true)
		}
	}
	tick()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			tick()
		}
	}()
}

// listenAndServe 始终 cmux：有面板证书时 HTTPS + HTTP→HTTPS；无证书时明文 HTTP。
func (s *Server) listenAndServe(handler http.Handler) error {
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}

	store := &certStore{}
	startCertReloader(store, s.onPanelTLSChange)

	tlsCfg := &tls.Config{
		GetCertificate: store.getCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
	}

	m := cmux.New(ln)
	tlsL := m.Match(cmux.TLS())
	httpL := m.Match(cmux.HTTP1Fast())
	anyL := m.Match(cmux.Any())

	httpsSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         tlsCfg,
	}
	httpSrv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store.get() != nil {
				host := r.Host
				if host == "" {
					host = s.Listen
				}
				http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusTemporaryRedirect)
				return
			}
			handler.ServeHTTP(w, r)
		}),
	}

	errCh := make(chan error, 3)
	go func() { errCh <- httpsSrv.Serve(tls.NewListener(tlsL, tlsCfg)) }()
	go func() { errCh <- httpSrv.Serve(httpL) }()
	go func() {
		for {
			c, err := anyL.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	mode := "http"
	if store.get() != nil {
		mode = "mux http/https"
	}
	log.Printf("master listening %s on %s (local panel=%s entrance=%s)", mode, s.Listen, s.LocalPanel, s.Entrance)
	if err := m.Serve(); err != nil {
		return err
	}
	return <-errCh
}

// onPanelTLSChange 在证书出现/消失时刷新上游 LocalPanel scheme 与反代 Transport。
func (s *Server) onPanelTLSChange(ready bool) {
	if s.InternalPort <= 0 {
		return
	}
	base := panel.LocalPanelURL(s.InternalPort)
	s.LocalPanel = base
	s.rebuildLocalProxy()
	log.Printf("local panel upstream -> %s (ssl_ready=%v)", base, ready)
}

// PublicScheme 返回对外推荐 scheme（安装命令 / Agent 用）。
func (s *Server) PublicScheme() string {
	if panel.PanelSSLReady() {
		return "https"
	}
	return "http"
}
