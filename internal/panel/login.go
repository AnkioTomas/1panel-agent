package panel

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// LoginResult 包含登录成功后的会话 Cookie。
type LoginResult struct {
	Cookies []*http.Cookie
}

// loginBody 对应 1Panel v2 登录 API 请求体。
type loginBody struct {
	Name          string `json:"name"`
	Password      string `json:"password"`
	IgnoreCaptcha bool   `json:"ignoreCaptcha"`
	Captcha       string `json:"captcha"`
	CaptchaID     string `json:"captchaID"`
	AuthMethod    string `json:"authMethod"`
	Language      string `json:"language"`
}

// apiResp 是 1Panel JSON API 的通用响应外壳。
type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Login 使用默认 HTTP 客户端登录 panelBase（如 http://127.0.0.1:52045）。
func Login(panelBase, entrance, username, password string) (*LoginResult, error) {
	return LoginWithClient(nil, panelBase, entrance, username, password, "")
}

// LoginWithClient 允许自定义 HTTP 客户端与可选公钥 PEM（隧道场景）。
//
// 1Panel ≥v2.0.14 不再信任 ignoreCaptcha：同一源 IP 登录失败后会强制验证码。
// Agent 连本机 127.0.0.1 时，失败几次就会把 loopback 锁死。
// 对策：对 loopback 目标使用 127.0.0.0/8 随机源地址拨号（Linux 合法），
// 每次登录落到干净的 IPTracker 桶；遇 ErrCaptchaCode 再换源重试。
func LoginWithClient(client *http.Client, panelBase, entrance, username, password, publicKeyPEM string) (*LoginResult, error) {
	base := strings.TrimRight(panelBase, "/")
	loopback := hostIsLoopback(base)

	var lastErr error
	attempts := 1
	if loopback {
		attempts = 4
	}
	for i := 0; i < attempts; i++ {
		c, err := buildLoginClient(client, loopback)
		if err != nil {
			return nil, err
		}
		res, err := loginOnce(c, base, entrance, username, password, publicKeyPEM)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !loopback || !isCaptchaErr(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func buildLoginClient(base *http.Client, loopback bool) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if base != nil && base.Transport != nil && !loopback {
		c.Transport = base.Transport
		return c, nil
	}
	if loopback {
		c.Transport = &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
				LocalAddr: &net.TCPAddr{IP: randomLoopbackIP(), Port: 0},
			}).DialContext,
		}
	}
	return c, nil
}

func loginOnce(c *http.Client, base, entrance, username, password, publicKeyPEM string) (*LoginResult, error) {
	entranceCode := ""
	if entrance != "" {
		entranceCode = base64.StdEncoding.EncodeToString([]byte(entrance))
		req, err := http.NewRequest(http.MethodGet, base+"/"+strings.TrimPrefix(entrance, "/"), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("EntranceCode", entranceCode)
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	if publicKeyPEM == "" {
		publicKeyPEM = publicKeyFromJar(c.Jar, base)
	}
	if publicKeyPEM == "" {
		if err := primePublicKey(c, base, entranceCode); err != nil {
			return nil, err
		}
		publicKeyPEM = publicKeyFromJar(c.Jar, base)
	}
	if publicKeyPEM == "" {
		return nil, fmt.Errorf("panel public key not found; open entrance first")
	}
	enc, err := EncryptPassword(password, publicKeyPEM)
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(loginBody{
		Name:          username,
		Password:      enc,
		IgnoreCaptcha: true, // 旧版仍认；新版忽略，改靠源 IP 隔离
		Captcha:       "",
		CaptchaID:     "mp",
		AuthMethod:    "session",
		Language:      "zh",
	})
	req, err := http.NewRequest(http.MethodPost, base+"/api/v2/core/auth/login", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if entranceCode != "" {
		req.Header.Set("EntranceCode", entranceCode)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var ar apiResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("login decode: %w body=%s", err, truncate(string(raw), 200))
	}
	if ar.Code != 200 {
		return nil, fmt.Errorf("login failed: %s", ar.Message)
	}

	u, _ := url.Parse(base)
	return &LoginResult{Cookies: c.Jar.Cookies(u)}, nil
}

// randomLoopbackIP 返回 127.0.0.0/8 中非 127.0.0.1 的地址，避开已被验证码锁定的 loopback 桶。
func randomLoopbackIP() net.IP {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := mrand.Uint32()
		b[0] = byte(n)
		b[1] = byte(n >> 8)
		b[2] = byte(n >> 16)
	}
	// 避开 127.0.0.0/24 里最常见的 .0/.1，降低与本机其他服务冲突概率
	if b[0] == 0 && b[1] == 0 && b[2] < 2 {
		b[2] = 2 + b[2]
	}
	return net.IPv4(127, b[0], b[1], b[2])
}

func hostIsLoopback(panelBase string) bool {
	u, err := url.Parse(panelBase)
	if err != nil {
		return strings.Contains(panelBase, "127.0.0.1") || strings.Contains(panelBase, "localhost")
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isCaptchaErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Captcha") || strings.Contains(s, "captcha")
}

// publicKeyFromJar 从 CookieJar 中取出 panel_public_key（PEM 文本）。
func publicKeyFromJar(jar http.CookieJar, base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == "panel_public_key" {
			v, err := url.QueryUnescape(c.Value)
			if err != nil {
				v = c.Value
			}
			raw, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return v
			}
			return string(raw)
		}
	}
	return ""
}

// primePublicKey 请求 /auth/captcha 以获取 panel_public_key，不计入登录失败。
func primePublicKey(c *http.Client, base, entranceCode string) error {
	req, err := http.NewRequest(http.MethodGet, base+"/api/v2/core/auth/captcha", nil)
	if err != nil {
		return err
	}
	if entranceCode != "" {
		req.Header.Set("EntranceCode", entranceCode)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

// truncate 截断过长字符串，用于错误信息。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
