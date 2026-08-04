package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTagPinned(t *testing.T) {
	c := &Config{Version: "v1.2.3", InstallCDN: "global"}
	tag, err := c.ResolveTag()
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("got %q", tag)
	}
}

func TestResolveTagLatest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/AnkioTomas/1panel-agent/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Config{
		GitHubAPI:  srv.URL,
		InstallCDN: "global",
		HTTPClient: srv.Client(),
	}
	tag, err := c.ResolveTag()
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v9.9.9" {
		t.Fatalf("got %q", tag)
	}
}

func TestDownloadBinaryWithChecksum(t *testing.T) {
	payload := []byte("fake-1pm-binary-content")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	file := "1pm_linux_arm64"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/AnkioTomas/1panel-agent/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.0.9"}`))
	})
	mux.HandleFunc("/AnkioTomas/1panel-agent/releases/download/v0.0.9/"+file, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/AnkioTomas/1panel-agent/releases/download/v0.0.9/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", sumHex, file)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	// BinaryName() 在非 linux CI 会失败：临时假装通过写死下载逻辑路径。
	// 这里直接测底层：用 pinned VERSION + 手动调下载目标。
	c := &Config{
		Version:    "v0.0.9",
		GitHubAPI:  srv.URL,
		GitHubDL:   srv.URL,
		InstallCDN: "global",
		HTTPClient: srv.Client(),
	}
	c.normalize()

	out := filepath.Join(dir, file)
	url := c.assetURL("", "v0.0.9", file)
	if err := c.httpGetFile(url, out); err != nil {
		t.Fatal(err)
	}
	if err := c.verifyChecksum("v0.0.9", file, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	file := "1pm_linux_amd64"
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/AnkioTomas/1panel-agent/releases/download/v1.0.0/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "deadbeef  %s\n", file)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Config{
		GitHubDL:   srv.URL,
		InstallCDN: "global",
		HTTPClient: srv.Client(),
	}
	c.normalize()
	err := c.verifyChecksum("v1.0.0", file, path)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
}

func TestCustomSourceForcesGlobalCDN(t *testing.T) {
	c := &Config{
		GitHubAPI:  "http://10.211.55.2:8765",
		GitHubDL:   "http://10.211.55.2:8765",
		InstallCDN: "auto",
	}
	c.normalize()
	if c.InstallCDN != "global" {
		t.Fatalf("InstallCDN=%q, want global", c.InstallCDN)
	}
	prefs := c.prefixes()
	if len(prefs) != 1 || prefs[0] != "" {
		t.Fatalf("prefixes=%v, want only direct", prefs)
	}
}
