package protocol

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// MaybeGunzip 在 Content-Encoding 为 gzip 时解压 body，否则原样返回。
func MaybeGunzip(body []byte, headers map[string][]string) []byte {
	if !strings.EqualFold(HeaderGet(headers, "Content-Encoding"), "gzip") {
		return body
	}
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		return body
	}
	return out
}

// HeaderGet 返回首个匹配的 header 值（大小写不敏感）。
func HeaderGet(headers map[string][]string, key string) string {
	want := http.CanonicalHeaderKey(key)
	for k, vals := range headers {
		if http.CanonicalHeaderKey(k) == want && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// CanStreamHTTP 判断响应是否可边收边转（无需 Master/Agent 整包缓冲）。
// HTML 要注入 Hook；JSON/401 要做会话失效探测——这些必须整包。
func CanStreamHTTP(status int, contentType string) bool {
	if status == http.StatusUnauthorized {
		return false
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "json") {
		return false
	}
	return true
}
