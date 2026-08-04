package master

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"1panel-agent/internal/panel"
)

func TestPublicSchemeFollowsPanelCert(t *testing.T) {
	dir := t.TempDir()
	panel.SetSecretDirForTest(dir)
	t.Cleanup(func() { panel.SetSecretDirForTest("") })

	s := &Server{}
	if s.PublicScheme() != "http" {
		t.Fatal("want http")
	}
	writeMasterTestCert(t, dir)
	if s.PublicScheme() != "https" {
		t.Fatal("want https")
	}
}

func TestHTTPRedirectWhenCertReady(t *testing.T) {
	store := &certStore{}
	// empty store → handler passes through
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store.get() != nil {
			http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		h.ServeHTTP(w, r)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://x/tomas", nil)
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("no cert: %d", rec.Code)
	}

	dir := t.TempDir()
	panel.SetSecretDirForTest(dir)
	t.Cleanup(func() { panel.SetSecretDirForTest("") })
	writeMasterTestCert(t, dir)
	cert, err := panel.LoadPanelTLS()
	if err != nil {
		t.Fatal(err)
	}
	store.set(cert)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://example:52045/tomas", nil)
	req.Host = "example:52045"
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("redirect: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example:52045/tomas" {
		t.Fatalf("loc %q", loc)
	}
}

func writeMasterTestCert(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "1pm-master-test"},
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
