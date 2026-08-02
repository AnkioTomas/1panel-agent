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
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := md5.Sum([]byte("1panel" + apiKey + ts))
	h.Set("1Panel-Token", hex.EncodeToString(sum[:]))
	h.Set("1Panel-Timestamp", ts)
}

func Token(apiKey string, unixTs int64) string {
	sum := md5.Sum([]byte("1panel" + apiKey + strconv.FormatInt(unixTs, 10)))
	return hex.EncodeToString(sum[:])
}
