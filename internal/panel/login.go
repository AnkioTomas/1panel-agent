package panel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	return LoginWithClient(http.DefaultClient, panelBase, entrance, username, password, "")
}

// LoginWithClient 允许自定义 HTTP 客户端与可选公钥 PEM（隧道场景）。
func LoginWithClient(client *http.Client, panelBase, entrance, username, password, publicKeyPEM string) (*LoginResult, error) {
	base := strings.TrimRight(panelBase, "/")
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := *client
	c.Jar = jar
	c.Timeout = 30 * time.Second
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

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
		publicKeyPEM = publicKeyFromJar(jar, base)
	}
	// Some 1Panel builds only emit panel_public_key on auth API responses.
	if publicKeyPEM == "" {
		if err := primePublicKey(&c, base, entranceCode); err != nil {
			return nil, err
		}
		publicKeyPEM = publicKeyFromJar(jar, base)
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
		IgnoreCaptcha: true,
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
	return &LoginResult{Cookies: jar.Cookies(u)}, nil
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
