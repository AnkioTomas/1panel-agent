package panel

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// secretDirOverride 供测试覆盖证书目录。
var (
	secretDirOverride string
	secretDirMu       sync.Mutex

	baseDirOnce sync.Once
	baseDirVal  string
)

// SetSecretDirForTest 仅测试使用：覆盖 SecretDir 探测结果；空字符串恢复默认。
func SetSecretDirForTest(dir string) {
	secretDirMu.Lock()
	secretDirOverride = dir
	secretDirMu.Unlock()
}

// ResetBaseDirForTest 仅测试使用：清空 BASE_DIR 缓存，便于测 1pctl / 环境变量。
func ResetBaseDirForTest() {
	baseDirOnce = sync.Once{}
	baseDirVal = ""
}

// BaseDir 返回 1Panel 安装根（1pctl 的 BASE_DIR）。
// 优先级：ONEPANEL_BASE_DIR → 解析 1pctl 的 BASE_DIR= → 默认 /opt。
// 安装时可改 BASE_DIR，证书在 {BaseDir}/1panel/secret。
func BaseDir() string {
	if base := strings.TrimSpace(os.Getenv("ONEPANEL_BASE_DIR")); base != "" {
		return base
	}
	baseDirOnce.Do(func() {
		baseDirVal = readBaseDirFrom1pctl()
		if baseDirVal == "" {
			baseDirVal = "/opt"
		}
	})
	return baseDirVal
}

// SecretDir 返回面板 SSL 证书目录（server.crt / server.key）。
func SecretDir() string {
	secretDirMu.Lock()
	override := secretDirOverride
	secretDirMu.Unlock()
	if override != "" {
		return override
	}
	return filepath.Join(BaseDir(), "1panel", "secret")
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

// PanelSSLReady 表示面板 secret 证书文件存在（用户已开启面板 SSL；关 SSL 会删文件）。
// 只 Stat，不解析证书；真正挂载时再用 LoadPanelTLS。
func PanelSSLReady() bool {
	certFile, keyFile := PanelCertPaths()
	_, err1 := os.Stat(certFile)
	_, err2 := os.Stat(keyFile)
	return err1 == nil && err2 == nil
}

// LocalPanelURL 按当前证书文件是否存在生成本地 1Panel 地址。
func LocalPanelURL(port int) string {
	return LocalPanelURLWithSSL(port, PanelSSLReady())
}

// LocalPanelURLWithSSL 按显式 SSL 状态生成本地 1Panel 地址。
func LocalPanelURLWithSSL(port int, ssl bool) string {
	scheme := "http"
	if ssl {
		scheme = "https"
	}
	return fmt.Sprintf("%s://127.0.0.1:%d", scheme, port)
}

// readBaseDirFrom1pctl 从 1pctl 脚本读取 BASE_DIR=（官方推荐方式）。
func readBaseDirFrom1pctl() string {
	candidates := make([]string, 0, 3)
	if p, err := exec.LookPath("1pctl"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, "/usr/local/bin/1pctl", "/usr/bin/1pctl")
	seen := map[string]struct{}{}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if b := parse1pctlBaseDirFile(p); b != "" {
			return b
		}
	}
	return ""
}

func parse1pctlBaseDirFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return parse1pctlBaseDir(f)
}

// parse1pctlBaseDir 从 1pctl 脚本内容解析 BASE_DIR= 行。
func parse1pctlBaseDir(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "BASE_DIR=") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "BASE_DIR="))
		v = strings.Trim(v, `"'`)
		if v != "" {
			return v
		}
	}
	return ""
}
