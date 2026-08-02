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

type LoginResult struct {
	Cookies []*http.Cookie
}

type loginBody struct {
	Name          string `json:"name"`
	Password      string `json:"password"`
	IgnoreCaptcha bool   `json:"ignoreCaptcha"`
	Captcha       string `json:"captcha"`
	CaptchaID     string `json:"captchaID"`
	AuthMethod    string `json:"authMethod"`
	Language      string `json:"language"`
}

type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Login performs 1Panel session login against panelBase (e.g. http://127.0.0.1:52045).
func Login(panelBase, entrance, username, password string) (*LoginResult, error) {
	return LoginWithClient(http.DefaultClient, panelBase, entrance, username, password, "")
}

// LoginWithClient allows a custom HTTP client (e.g. tunnel transport) and optional public key PEM.
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

	if entrance != "" {
		req, err := http.NewRequest(http.MethodGet, base+"/"+strings.TrimPrefix(entrance, "/"), nil)
		if err != nil {
			return nil, err
		}
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
	if entrance != "" {
		req.Header.Set("EntranceCode", base64.StdEncoding.EncodeToString([]byte(entrance)))
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
