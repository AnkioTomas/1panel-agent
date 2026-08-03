package master

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"1panel-agent/internal/release"
)

func TestReplaceMasterBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only binary naming")
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		t.Skip("unsupported arch")
	}
	file := "1pm_linux_" + arch
	payload := []byte("new-master-binary")
	sum := sha256.Sum256(payload)

	mux := http.NewServeMux()
	mux.HandleFunc("/AnkioTomas/1panel-agent/releases/download/v0.0.8-test/"+file, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/AnkioTomas/1panel-agent/releases/download/v0.0.8-test/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), file)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	exe := filepath.Join(dir, "1pm")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &release.Config{
		Version:    "v0.0.8-test",
		GitHubAPI:  srv.URL,
		GitHubDL:   srv.URL,
		InstallCDN: "global",
		HTTPClient: srv.Client(),
	}
	tag, err := replaceMasterBinary(exe, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.0.8-test" {
		t.Fatalf("tag %q", tag)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("binary not replaced")
	}
}

func TestHandleUpdateMasterMethod(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__mp/api/update-master", nil)
	s.handleUpdateMaster(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rec.Code)
	}
}
