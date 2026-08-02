package master

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	authCookie = "mp_auth"
	authTTL    = 7 * 24 * time.Hour
	authSkew   = 5 * time.Minute
)

func (s *Server) authSecret() string {
	// Token doubles as HMAC secret; never expose in UI.
	return s.Token
}

func (s *Server) issueAuthCookie(w http.ResponseWriter) {
	exp := time.Now().Add(authTTL).Unix()
	payload := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(s.authSecret()))
	_, _ = mac.Write([]byte(payload))
	val := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(authTTL.Seconds()),
	})
}

func (s *Server) validAuthCookie(r *http.Request) bool {
	c, err := r.Cookie(authCookie)
	if err != nil || c.Value == "" {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.authSecret()))
	_, _ = mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(parts[1]))
}

// localPanelLoggedIn checks the caller's cookies against the local 1Panel upstream.
func (s *Server) localPanelLoggedIn(r *http.Request) bool {
	if s.LocalPanel == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(s.LocalPanel, "/")+"/api/v2/dashboard/base/os", nil)
	if err != nil {
		return false
	}
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if s.Entrance != "" {
		req.Header.Set("EntranceCode", base64.StdEncoding.EncodeToString([]byte(s.Entrance)))
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var ar struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &ar); err != nil {
		return false
	}
	return ar.Code == 200
}

// requireMPAuth allows access if mp_auth is valid, or if local 1Panel session is valid (then issues mp_auth).
func (s *Server) requireMPAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.LocalPanel == "" {
		return true
	}
	if s.validAuthCookie(r) {
		return true
	}
	if s.localPanelLoggedIn(r) {
		s.issueAuthCookie(w)
		return true
	}
	// Not authorized: send to local panel login.
	target := "/"
	if s.Entrance != "" {
		target = "/" + strings.TrimPrefix(s.Entrance, "/")
	}
	// Clear remote node so login hits local panel.
	http.SetCookie(w, &http.Cookie{Name: "mp_node", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, target+"?mp_return=/__mp/", http.StatusFound)
	return false
}

func (s *Server) denyAPI(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"code":401,"message":%q}`, msg)
}
