package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"

	"1panel-agent/internal/panel"
)

func main() {
	base := os.Getenv("BASE")
	entrance := os.Getenv("ENTRANCE")
	if entrance == "" {
		entrance = "tomas"
	}
	res, err := panel.Login(base, entrance, "ankio", "ankio@2026.8")
	if err != nil {
		fmt.Println("login", err)
		os.Exit(1)
	}
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(base)
	jar.SetCookies(u, res.Cookies)
	c := &http.Client{Jar: jar}
	csrf := ""
	for _, ck := range res.Cookies {
		if strings.EqualFold(ck.Name, "pcsrftoken") {
			csrf = ck.Value
		}
	}
	req, _ := http.NewRequest("GET", base+"/api/v2/core/settings/upgrade", nil)
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Println("get", err)
		os.Exit(1)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("status", resp.StatusCode)
	fmt.Println(string(b))
}
