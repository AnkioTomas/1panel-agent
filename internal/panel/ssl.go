package panel

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// secretDirOverride 供测试覆盖证书目录。
var (
	secretDirOverride string
	secretDirMu       sync.Mutex
)

// SetSecretDirForTest 仅测试使用：覆盖 SecretDir 探测结果；空字符串恢复默认。
func SetSecretDirForTest(dir string) {
	secretDirMu.Lock()
	secretDirOverride = dir
	secretDirMu.Unlock()
}

// SecretDir 返回 1Panel 面板 SSL 证书目录（server.crt / server.key）。
// 对应 1pctl BASE_DIR（默认 /opt）下的 1panel/secret，即 /opt/1panel/secret。
// 环境变量 ONEPANEL_BASE_DIR 覆盖 BASE_DIR（仍拼 1panel/secret）。
func SecretDir() string {
	secretDirMu.Lock()
	override := secretDirOverride
	secretDirMu.Unlock()
	if override != "" {
		return override
	}
	base := os.Getenv("ONEPANEL_BASE_DIR")
	if base == "" {
		base = "/opt"
	}
	return filepath.Join(base, "1panel", "secret")
}

// PanelCertPaths 返回面板证书与私钥路径。
func PanelCertPaths() (certFile, keyFile string) {
	dir := SecretDir()
	return filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
}

// LoadPanelTLS 从面板 secret 目录加载 TLS 证书对。
func LoadPanelTLS() (*tls.Certificate, error) {
	certFile, keyFile := PanelCertPaths()
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse panel tls: %w", err)
	}
	return &cert, nil
}

// PanelSSLReady 表示面板 secret 证书可加载（用户已开启面板 SSL）。
func PanelSSLReady() bool {
	_, err := LoadPanelTLS()
	return err == nil
}

// LocalPanelURL 生成指定端口的本地 1Panel 地址；面板 SSL 就绪时用 https。
func LocalPanelURL(port int) string {
	scheme := "http"
	if PanelSSLReady() {
		scheme = "https"
	}
	return fmt.Sprintf("%s://127.0.0.1:%d", scheme, port)
}
