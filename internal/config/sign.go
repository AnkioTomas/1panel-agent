package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// Sign 计算与 Master/Agent 共用的 HMAC-SHA256 签名（hex）。
// 消息格式固定为：timestamp=<unix>。
func Sign(secret, timestampStr string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("timestamp=" + timestampStr))
	return hex.EncodeToString(mac.Sum(nil))
}

// SignOK 常量时间比较签名是否匹配。
func SignOK(secret, timestampStr, sign string) bool {
	if secret == "" || timestampStr == "" || sign == "" {
		return false
	}
	expected := Sign(secret, timestampStr)
	return subtle.ConstantTimeCompare([]byte(sign), []byte(expected)) == 1
}
