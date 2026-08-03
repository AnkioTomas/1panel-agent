package panel

import (
	"net"
	"testing"
)

func TestHostIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"http://127.0.0.1:52045":    true,
		"http://localhost:52045":    true,
		"http://10.211.55.15:52045": false,
		"http://[::1]:52045":        true,
	}
	for in, want := range cases {
		if got := hostIsLoopback(in); got != want {
			t.Fatalf("%s: got %v want %v", in, got, want)
		}
	}
}

func TestRandomLoopbackIP(t *testing.T) {
	for range 50 {
		ip := randomLoopbackIP()
		if !ip.IsLoopback() {
			t.Fatalf("not loopback: %v", ip)
		}
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			t.Fatal("must not return 127.0.0.1")
		}
	}
}

func TestIsCaptchaErr(t *testing.T) {
	if !isCaptchaErr(errString("login failed: ErrCaptchaCode")) {
		t.Fatal("expected captcha")
	}
	if isCaptchaErr(errString("login failed: ErrAuth")) {
		t.Fatal("not captcha")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
