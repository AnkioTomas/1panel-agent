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

// DeleteHeader 删除所有大小写变体的指定头。
func DeleteHeader(headers map[string][]string, key string) {
	want := http.CanonicalHeaderKey(key)
	delete(headers, key)
	delete(headers, want)
	for k := range headers {
		if http.CanonicalHeaderKey(k) == want {
			delete(headers, k)
		}
	}
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

// IsCacheableAsset 识别应长期缓存的静态资源类型。
// 这类响应禁止带 Set-Cookie，否则浏览器 disk cache 直接罢工。
func IsCacheableAsset(contentType string) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "javascript"),
		strings.Contains(ct, "ecmascript"),
		strings.Contains(ct, "text/css"),
		strings.Contains(ct, "image/"),
		strings.Contains(ct, "font/"),
		strings.Contains(ct, "woff"),
		strings.Contains(ct, "wasm"),
		strings.Contains(ct, "audio/"),
		strings.Contains(ct, "video/"),
		strings.Contains(ct, "application/wasm"):
		return true
	default:
		return false
	}
}
