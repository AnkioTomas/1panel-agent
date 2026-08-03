package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const secretKeyFile = "secret.key"

// secretKeyPath 返回本地加密密钥路径（与 agent.json 同目录）。
func secretKeyPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, secretKeyFile), nil
}

// loadOrCreateSecretKey 读取或生成 32 字节 AES 密钥（文件权限 0600）。
func loadOrCreateSecretKey() ([]byte, error) {
	path, err := secretKeyPath()
	if err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) == 32 {
			return data, nil
		}
		return nil, fmt.Errorf("invalid secret key length in %s", path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// EncryptSecret 用本地密钥 AES-GCM 加密，返回 base64(nonce|ciphertext)。
func EncryptSecret(plain string) (string, error) {
	key, err := loadOrCreateSecretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptSecret 解密 EncryptSecret 的产物。
func DecryptSecret(enc string) (string, error) {
	if enc == "" {
		return "", fmt.Errorf("empty ciphertext")
	}
	key, err := loadOrCreateSecretKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
