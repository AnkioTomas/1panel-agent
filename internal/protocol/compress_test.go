package protocol

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestMaybeGunzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("hello-gzip"))
	_ = zw.Close()

	got := MaybeGunzip(buf.Bytes(), map[string][]string{"Content-Encoding": {"gzip"}})
	if string(got) != "hello-gzip" {
		t.Fatalf("gunzip got %q", got)
	}
	plain := []byte("plain")
	if string(MaybeGunzip(plain, nil)) != "plain" {
		t.Fatal("identity changed")
	}
}

func TestCanStreamHTTP(t *testing.T) {
	cases := []struct {
		status int
		ct     string
		want   bool
	}{
		{200, "application/javascript", true},
		{200, "text/css", true},
		{200, "image/png", true},
		{200, "text/html; charset=utf-8", false},
		{200, "application/json", false},
		{401, "application/javascript", false},
		{200, "", true},
	}
	for _, tc := range cases {
		if got := CanStreamHTTP(tc.status, tc.ct); got != tc.want {
			t.Fatalf("status=%d ct=%q got=%v want=%v", tc.status, tc.ct, got, tc.want)
		}
	}
}

func TestIsCacheableAsset(t *testing.T) {
	if !IsCacheableAsset("application/javascript") || !IsCacheableAsset("text/css") {
		t.Fatal("js/css should be assets")
	}
	if IsCacheableAsset("text/html") || IsCacheableAsset("") || IsCacheableAsset("text/plain") {
		t.Fatal("non-assets misclassified")
	}
}

func TestHeaderGet(t *testing.T) {
	h := map[string][]string{"content-type": {"text/css"}}
	if HeaderGet(h, "Content-Type") != "text/css" {
		t.Fatal(HeaderGet(h, "Content-Type"))
	}
}

func TestDeleteHeader(t *testing.T) {
	h := map[string][]string{
		"Set-Cookie":    {"a=1"},
		"set-cookie":    {"b=2"},
		"Cache-Control": {"max-age=1"},
	}
	DeleteHeader(h, "Set-Cookie")
	if HeaderGet(h, "Set-Cookie") != "" || len(h["set-cookie"]) > 0 {
		t.Fatalf("Set-Cookie not deleted: %#v", h)
	}
	if HeaderGet(h, "Cache-Control") != "max-age=1" {
		t.Fatal("Cache-Control lost")
	}
}
