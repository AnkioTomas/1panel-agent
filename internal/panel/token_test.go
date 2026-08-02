package panel

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"testing"
)

func TestToken(t *testing.T) {
	key := "secret"
	ts := int64(1700000000)
	got := Token(key, ts)
	sum := md5.Sum([]byte("1panel" + key + "1700000000"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestInjectAuth(t *testing.T) {
	h := make(http.Header)
	InjectAuth(h, "secret")
	if h.Get("1Panel-Token") == "" || h.Get("1Panel-Timestamp") == "" {
		t.Fatal("headers not set")
	}
	tok := h.Get("1Panel-Token")
	InjectAuth(h, "other")
	if h.Get("1Panel-Token") != tok {
		t.Fatal("should not overwrite existing token")
	}
}
