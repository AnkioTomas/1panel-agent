package panel

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

// InjectAuth adds 1Panel API auth headers when key is set and headers are absent.
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

func Token(apiKey string, unixTs int64) string {
	sum := md5.Sum([]byte("1panel" + apiKey + strconv.FormatInt(unixTs, 10)))
	return hex.EncodeToString(sum[:])
}
