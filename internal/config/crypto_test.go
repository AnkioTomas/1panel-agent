package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSignRoundTrip(t *testing.T) {
	sig := Sign("secret", "1700000000")
	if !SignOK("secret", "1700000000", sig) {
		t.Fatal("sign mismatch")
	}
	if SignOK("secret", "1700000000", "deadbeef") {
		t.Fatal("bad sign accepted")
	}
}

func TestEncryptSecretRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Dir() uses UserHomeDir → HOME
	enc, err := EncryptSecret("p@ss")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptSecret(enc)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "p@ss" {
		t.Fatalf("got %q", plain)
	}
	keyPath := filepath.Join(home, dirName, secretKeyFile)
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("secret key missing: %v", err)
	}
}
