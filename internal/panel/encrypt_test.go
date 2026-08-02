package panel

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestEncryptPasswordShape(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	out, err := EncryptPassword("ankio@2026.8", pubPEM)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(out, ":")
	if len(parts) != 3 {
		t.Fatalf("want 3 parts, got %d: %q", len(parts), out)
	}
	for i, p := range parts {
		if p == "" {
			t.Fatalf("part %d empty", i)
		}
	}
}
