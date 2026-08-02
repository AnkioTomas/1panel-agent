package panel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"strings"
)

// EncryptPassword matches 1Panel frontend encryptPassword (RSA+AES hybrid).
func EncryptPassword(plain string, publicKeyPEM string) (string, error) {
	pub, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}
	aesKey := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return "", err
	}
	aesKeyHex := hex.EncodeToString(aesKey) // 32-char string used as AES-256 key bytes

	keyCipher, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(aesKeyHex))
	if err != nil {
		return "", err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(aesKeyHex))
	if err != nil {
		return "", err
	}
	pad := pkcs7Pad([]byte(plain), aes.BlockSize)
	ct := make([]byte, len(pad))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, pad)

	return base64.StdEncoding.EncodeToString(keyCipher) + ":" +
		base64.StdEncoding.EncodeToString(iv) + ":" +
		base64.StdEncoding.EncodeToString(ct), nil
}

func parsePublicKey(pemText string) (*rsa.PublicKey, error) {
	pemText = strings.ReplaceAll(pemText, `"`, "")
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		// maybe base64-wrapped PEM
		raw, err := base64.StdEncoding.DecodeString(pemText)
		if err != nil {
			return nil, fmt.Errorf("invalid public key pem")
		}
		block, _ = pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("invalid public key pem")
		}
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not RSA public key")
	}
	return pub, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	n := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+n)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(n)
	}
	return out
}
