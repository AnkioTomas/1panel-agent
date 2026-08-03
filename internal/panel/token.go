package panel

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

// InjectAuth 在 apiKey 非空且头未设置时注入 1Panel-Token / 1Panel-Timestamp。
func InjectAuth(h http.Header, apiKey string) {
	if apiKey == "" {
		return
	}
	if h.Get("1Panel-Token") != "" {
		return
	}
	ts := time.Now().Unix()
	h.Set("1Panel-Token", Token(apiKey, ts))
	h.Set("1Panel-Timestamp", strconv.FormatInt(ts, 10))
}

// Token 按 1Panel 规则计算 md5("1panel"+apiKey+timestamp)。
func Token(apiKey string, unixTs int64) string {
	sum := md5.Sum([]byte("1panel" + apiKey + strconv.FormatInt(unixTs, 10)))
	return hex.EncodeToString(sum[:])
}
