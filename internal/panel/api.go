package panel

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// NewInsecureClient 返回跳过 TLS 校验的短超时客户端（本机自签面板常用）。
func NewInsecureClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// ApplyEntrance 在启用安全入口时注入 EntranceCode。
func ApplyEntrance(h http.Header, entrance string) {
	if entrance == "" || h.Get("EntranceCode") != "" {
		return
	}
	h.Set("EntranceCode", base64.StdEncoding.EncodeToString([]byte(entrance)))
}

// AlignCSRF 使 X-CSRF-Token 与 Cookie 中的 pcsrftoken 一致。
func AlignCSRF(h http.Header) {
	csrf := CookieValue(h.Get("Cookie"), "pcsrftoken")
	if csrf == "" {
		h.Del("X-CSRF-Token")
		return
	}
	h.Set("X-CSRF-Token", csrf)
}

// CookieValue 从 Cookie 头解析指定名称的值。
func CookieValue(cookieHeader, name string) string {
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			continue
		}
		if strings.TrimSpace(part[:eq]) == name {
			return strings.TrimSpace(part[eq+1:])
		}
	}
	return ""
}

// SessionCookieNames 是 1Panel 会话相关 Cookie 名（切节点时不得混用）。
var SessionCookieNames = []string{
	"psession",
	"pcsrftoken",
	"securityentrance",
	"panel_public_key",
}

// IsSessionCookie 判断是否为 1Panel 会话相关 Cookie。
func IsSessionCookie(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return slices.Contains(SessionCookieNames, n)
}

func setCookies(req *http.Request, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	if len(parts) == 0 {
		return
	}
	req.Header.Set("Cookie", strings.Join(parts, "; "))
}

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func doJSON(client *http.Client, method, urlStr string, entrance string, cookies []*http.Cookie, body any) (*apiEnvelope, []byte, error) {
	if client == nil {
		client = NewInsecureClient(0)
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, urlStr, rdr)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	setCookies(req, cookies)
	ApplyEntrance(req.Header, entrance)
	AlignCSRF(req.Header)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, raw, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var ar apiEnvelope
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, raw, fmt.Errorf("decode: %w body=%s", err, truncate(string(raw), 200))
	}
	if ar.Code != 200 {
		return &ar, raw, fmt.Errorf("%s", ar.Message)
	}
	return &ar, raw, nil
}

// UpgradeInfo 对应 GET /api/v2/core/settings/upgrade 的 data。
type UpgradeInfo struct {
	TestVersion   string `json:"testVersion"`
	NewVersion    string `json:"newVersion"`
	LatestVersion string `json:"latestVersion"`
	ReleaseNote   string `json:"releaseNote"`
}

// PickUpgradeVersion 从升级信息中选目标版本。
func PickUpgradeVersion(info UpgradeInfo) string {
	for _, v := range []string{info.NewVersion, info.LatestVersion, info.TestVersion} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// UpgradeResult 是本机面板升级结果。
type UpgradeResult struct {
	OK            bool   `json:"ok"`
	Skipped       bool   `json:"skipped,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	Message       string `json:"message,omitempty"`
}

// Upgrade 用已登录会话触发官方升级；已最新则 Skipped。
func Upgrade(client *http.Client, base, entrance string, cookies []*http.Cookie, forceVersion string) (*UpgradeResult, error) {
	base = strings.TrimRight(base, "/")
	ar, _, err := doJSON(client, http.MethodGet, base+"/api/v2/core/settings/upgrade", entrance, cookies, nil)
	if err != nil {
		return nil, fmt.Errorf("upgrade info: %w", err)
	}
	var info UpgradeInfo
	if len(ar.Data) > 0 {
		_ = json.Unmarshal(ar.Data, &info)
	}
	target := strings.TrimSpace(forceVersion)
	if target == "" {
		target = PickUpgradeVersion(info)
	}
	if target == "" {
		return &UpgradeResult{OK: true, Skipped: true, Message: "already up to date"}, nil
	}
	_, _, err = doJSON(client, http.MethodPost, base+"/api/v2/core/settings/upgrade", entrance, cookies, map[string]string{
		"version": target,
	})
	if err != nil {
		return nil, fmt.Errorf("upgrade post: %w", err)
	}
	return &UpgradeResult{OK: true, TargetVersion: target, Message: "upgrade started"}, nil
}

// UpdateSSL 开关本机面板自签 SSL（sslType=self）。
func UpdateSSL(client *http.Client, base, entrance string, cookies []*http.Cookie, enable bool, domain string) error {
	base = strings.TrimRight(base, "/")
	mode := "Disable"
	if enable {
		mode = "Enable"
	}
	body := map[string]any{
		"sslType": "self",
		"ssl":     mode,
		"domain":  domain,
		"sslID":   0,
	}
	_, _, err := doJSON(client, http.MethodPost, base+"/api/v2/core/settings/ssl/update", entrance, cookies, body)
	if err != nil {
		return fmt.Errorf("ssl update: %w", err)
	}
	return nil
}

// UpdateBindInfo 更新面板监听地址（如 127.0.0.1）；面板会异步重启。
func UpdateBindInfo(client *http.Client, base, entrance string, cookies []*http.Cookie, bindAddress string) error {
	base = strings.TrimRight(base, "/")
	if bindAddress == "" {
		bindAddress = "127.0.0.1"
	}
	body := map[string]any{
		"ipv6":        "Disable",
		"bindAddress": bindAddress,
	}
	_, _, err := doJSON(client, http.MethodPost, base+"/api/v2/core/settings/bind/update", entrance, cookies, body)
	if err != nil {
		return fmt.Errorf("bind update: %w", err)
	}
	return nil
}
