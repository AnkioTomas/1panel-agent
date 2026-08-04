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
	t.Setenv("ONEPANEL_BASE_DIR", "")
	if got := SecretDir(); got != "/opt/1panel/secret" {
		t.Fatalf("default SecretDir=%q", got)
	}
	t.Setenv("ONEPANEL_BASE_DIR", "/data")
	if got := SecretDir(); got != "/data/1panel/secret" {
		t.Fatalf("env SecretDir=%q", got)
	}
}
