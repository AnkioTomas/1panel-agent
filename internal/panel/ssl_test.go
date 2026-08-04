package panel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestCertPair(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "1pm-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, "server.crt"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.key"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPanelSSLReadyAndLocalPanelURL(t *testing.T) {
	dir := t.TempDir()
	SetSecretDirForTest(dir)
	t.Cleanup(func() { SetSecretDirForTest("") })

	if PanelSSLReady() {
		t.Fatal("empty dir should not be ready")
	}
	if u := LocalPanelURL(62045); u != "http://127.0.0.1:62045" {
		t.Fatalf("http url: %s", u)
	}
	if u := LocalPanelURLWithSSL(62045, false); u != "http://127.0.0.1:62045" {
		t.Fatalf("explicit http: %s", u)
	}

	writeTestCertPair(t, dir)
	if !PanelSSLReady() {
		t.Fatal("expected ssl ready")
	}
	cert, err := LoadPanelTLS()
	if err != nil || cert == nil {
		t.Fatalf("load: %v", err)
	}
	if u := LocalPanelURL(62045); u != "https://127.0.0.1:62045" {
		t.Fatalf("https url: %s", u)
	}
	if u := LocalPanelURLWithSSL(62045, true); u != "https://127.0.0.1:62045" {
		t.Fatalf("explicit https: %s", u)
	}
}

func TestLoadPanelTLSMissing(t *testing.T) {
	SetSecretDirForTest(t.TempDir())
	t.Cleanup(func() { SetSecretDirForTest("") })
	if _, err := LoadPanelTLS(); err == nil {
		t.Fatal("expected error")
	}
}

func TestSecretDirDefault(t *testing.T) {
	SetSecretDirForTest("")
	t.Cleanup(func() { SetSecretDirForTest("") })
	ResetBaseDirForTest()
	t.Cleanup(ResetBaseDirForTest)

	t.Setenv("ONEPANEL_BASE_DIR", "/data")
	if got := SecretDir(); got != "/data/1panel/secret" {
		t.Fatalf("env SecretDir=%q", got)
	}

	t.Setenv("ONEPANEL_BASE_DIR", "")
	ResetBaseDirForTest()
	// 无 1pctl / 无环境变量时回落 /opt（与官方默认一致）
	if got := BaseDir(); got == "" {
		t.Fatal("BaseDir empty")
	}
	if got := SecretDir(); !strings.HasSuffix(got, "/1panel/secret") {
		t.Fatalf("SecretDir=%q", got)
	}
}

func TestParse1pctlBaseDir(t *testing.T) {
	in := "#!/bin/bash\n# comment\nBASE_DIR=/data/disk1\nORIGINAL_PORT=52045\n"
	if got := parse1pctlBaseDir(strings.NewReader(in)); got != "/data/disk1" {
		t.Fatalf("got %q", got)
	}
	if got := parse1pctlBaseDir(strings.NewReader("BASE_DIR=\"/opt\"\n")); got != "/opt" {
		t.Fatalf("quoted got %q", got)
	}
	if got := parse1pctlBaseDir(strings.NewReader("FOO=1\n")); got != "" {
		t.Fatalf("empty expected, got %q", got)
	}
}
