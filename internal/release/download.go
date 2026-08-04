// Package release 从 GitHub Release（或兼容的 GITHUB_API/GITHUB_DL）拉取 1pm 二进制。
// 环境变量与 install.sh 对齐：REPO、VERSION、GITHUB_API、GITHUB_DL、INSTALL_CDN。
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultRepo = "AnkioTomas/1panel-agent"

var mirrorPrefixes = []string{
	"https://gh-proxy.com/",
	"https://ghfast.top/",
	"https://ghproxy.net/",
	"https://cdn.gh-proxy.com/",
	"https://gh.dpik.top/",
	"https://gh.monlor.com/",
	"https://gh.noki.icu/",
	"https://gh.tryxd.cn/",
	"https://ghpr.cc/",
	"https://gitproxy.click/",
}

// Config 控制下载来源；字段为空时读环境变量或默认值。
type Config struct {
	Repo       string
	Version    string // 固定 tag；空则解析 latest
	GitHubAPI  string
	GitHubDL   string
	InstallCDN string // auto | global | cn
	HTTPClient *http.Client
}

func (c *Config) normalize() {
	if c.Repo == "" {
		c.Repo = envOr("REPO", defaultRepo)
	}
	if c.Version == "" {
		c.Version = os.Getenv("VERSION")
	}
	if c.GitHubAPI == "" {
		c.GitHubAPI = envOr("GITHUB_API", "https://api.github.com")
	}
	if c.GitHubDL == "" {
		c.GitHubDL = envOr("GITHUB_DL", "https://github.com")
	}
	if c.InstallCDN == "" {
		c.InstallCDN = envOr("INSTALL_CDN", "auto")
	}
	c.GitHubAPI = strings.TrimRight(c.GitHubAPI, "/")
	c.GitHubDL = strings.TrimRight(c.GitHubDL, "/")
	// 自定义 API/下载源时禁止走 gh-proxy 镜像前缀，否则 URL 会被拼坏。
	if (c.GitHubAPI != "" && c.GitHubAPI != "https://api.github.com") ||
		(c.GitHubDL != "" && c.GitHubDL != "https://github.com") {
		if c.InstallCDN == "" || c.InstallCDN == "auto" {
			c.InstallCDN = "global"
		}
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 3 * time.Minute}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// BinaryName 返回当前 GOOS/GOARCH 对应的 Release 资源名。
func BinaryName() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("master self-update supports linux only (got %s)", runtime.GOOS)
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported arch: %s", arch)
	}
	return "1pm_linux_" + arch, nil
}

func (c *Config) prefixes() []string {
	switch c.InstallCDN {
	case "global":
		return []string{""}
	case "cn":
		out := append([]string{}, mirrorPrefixes...)
		return append(out, "")
	default: // auto
		out := append([]string{}, mirrorPrefixes...)
		return append(out, "")
	}
}

func (c *Config) assetURL(prefix, tag, file string) string {
	base := fmt.Sprintf("%s/%s/releases/download/%s/%s", c.GitHubDL, c.Repo, tag, file)
	if prefix == "" {
		return base
	}
	return prefix + base
}

func (c *Config) latestAPIURL(prefix string) string {
	base := fmt.Sprintf("%s/repos/%s/releases/latest", c.GitHubAPI, c.Repo)
	if prefix == "" {
		return base
	}
	return prefix + base
}

// ResolveTag 返回要下载的 release tag。
func (c *Config) ResolveTag() (string, error) {
	c.normalize()
	if c.Version != "" {
		tag := strings.TrimSpace(c.Version)
		if err := assertCleanTag(tag); err != nil {
			return "", err
		}
		return tag, nil
	}
	var lastErr error
	for _, prefix := range c.prefixes() {
		tag, err := c.fetchLatestTag(prefix)
		if err != nil {
			lastErr = err
			continue
		}
		if err := assertCleanTag(tag); err != nil {
			lastErr = err
			continue
		}
		return tag, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("cannot resolve latest release")
	}
	return "", lastErr
}

func assertCleanTag(tag string) error {
	if tag == "" || strings.ContainsAny(tag, " \n") || strings.Contains(tag, "==>") {
		return fmt.Errorf("invalid release tag: %q", tag)
	}
	if !strings.HasPrefix(tag, "v") {
		return fmt.Errorf("invalid release tag: %q", tag)
	}
	return nil
}

func (c *Config) fetchLatestTag(prefix string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.latestAPIURL(prefix), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("latest API %s: status %d", c.latestAPIURL(prefix), resp.StatusCode)
	}
	var meta struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", err
	}
	if meta.TagName == "" {
		return "", fmt.Errorf("latest API missing tag_name")
	}
	return meta.TagName, nil
}

// Result 是一次成功下载的结果。
type Result struct {
	Tag      string
	FileName string
	Path     string
}

// DownloadBinary 下载当前平台二进制到 destDir，返回落盘路径。
func (c *Config) DownloadBinary(destDir string) (*Result, error) {
	c.normalize()
	name, err := BinaryName()
	if err != nil {
		return nil, err
	}
	tag, err := c.ResolveTag()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	outPath := filepath.Join(destDir, name)

	var lastErr error
	ok := false
	for _, prefix := range c.prefixes() {
		url := c.assetURL(prefix, tag, name)
		if err := c.httpGetFile(url, outPath); err != nil {
			lastErr = err
			continue
		}
		ok = true
		break
	}
	if !ok {
		if lastErr == nil {
			lastErr = fmt.Errorf("download failed: %s", name)
		}
		return nil, lastErr
	}

	// 可选 checksum：失败则报错；文件不存在则跳过。
	if err := c.verifyChecksum(tag, name, outPath); err != nil {
		_ = os.Remove(outPath)
		return nil, err
	}
	return &Result{Tag: tag, FileName: name, Path: outPath}, nil
}

func (c *Config) verifyChecksum(tag, file, path string) error {
	var sumData []byte
	var got bool
	for _, prefix := range c.prefixes() {
		url := c.assetURL(prefix, tag, "checksums.txt")
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || len(body) == 0 {
			continue
		}
		sumData = body
		got = true
		break
	}
	if !got {
		return nil
	}
	want := ""
	for _, line := range strings.Split(string(sumData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == file {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	gotSum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(gotSum, want) {
		return fmt.Errorf("checksum mismatch for %s: want %s got %s", file, want, gotSum)
	}
	return nil
}

func (c *Config) httpGetFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "1pm-dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	n, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s: empty body", url)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	return nil
}
