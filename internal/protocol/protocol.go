// Package protocol 定义 Master/Agent 隧道上的注册消息、流元数据与分块传输格式。
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xtaci/smux"
)

// 隧道流类型与帧大小上限。
const (
	// StreamTypeHTTP 表示普通 HTTP 隧道流。
	StreamTypeHTTP byte = 1
	// StreamTypeWS 表示 WebSocket 隧道流。
	StreamTypeWS byte = 2
	// StreamTypeStats 表示 Master 拉取 Agent 主机状态（CPU/内存/版本）。
	StreamTypeStats byte = 3
	maxJSONFrame         = 16 << 20
	maxChunk             = 8 << 20
)

// Register 是 Agent 在 WebSocket 升级后发送的首条 JSON 注册消息。
type Register struct {
	ID           string `json:"id"`
	Hostname     string `json:"hostname"`
	PanelURL     string `json:"panel_url"`
	PanelVersion string `json:"panel_version,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
}

// HostStats 是 Agent 上报的主机状态快照。
type HostStats struct {
	CPUPercent   float64 `json:"cpu_percent"`
	MemTotal     uint64  `json:"mem_total"`
	MemUsed      uint64  `json:"mem_used"`
	AgentVersion string  `json:"agent_version,omitempty"`
	PanelVersion string  `json:"panel_version,omitempty"`
	GOOS         string  `json:"goos,omitempty"`
	GOARCH       string  `json:"goarch,omitempty"`
}

// RegisterOK 是 Master 在 smux 接管前的注册应答。
type RegisterOK struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RequestMeta 写在每条 Master→Agent smux 流开头。
type RequestMeta struct {
	Type    byte                `json:"-"`
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
}

// ResponseMeta 写在每条 HTTP 响应流开头。
type ResponseMeta struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
}

// WriteJSON 以 4 字节大端长度前缀写入 JSON 帧。
func WriteJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadJSON 读取一条长度前缀 JSON 帧并反序列化到 v。
func ReadJSON(r io.Reader, v any) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n == 0 || n > maxJSONFrame {
		return fmt.Errorf("invalid json frame length: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}

// WriteRequestMeta 写入流类型字节 + RequestMeta JSON。
func WriteRequestMeta(w io.Writer, meta *RequestMeta) error {
	if meta.Type == 0 {
		meta.Type = StreamTypeHTTP
	}
	if _, err := w.Write([]byte{meta.Type}); err != nil {
		return err
	}
	return WriteJSON(w, meta)
}

// ReadRequestMeta 读取流类型字节与 RequestMeta。
func ReadRequestMeta(r io.Reader) (*RequestMeta, error) {
	var typ [1]byte
	if _, err := io.ReadFull(r, typ[:]); err != nil {
		return nil, err
	}
	meta := &RequestMeta{Type: typ[0]}
	if err := ReadJSON(r, meta); err != nil {
		return nil, err
	}
	meta.Type = typ[0]
	return meta, nil
}

// CopyChunks 将 r 写成长度前缀分块，并以零长度块结束。
func CopyChunks(w io.Writer, r io.Reader) error {
	buf := make([]byte, 32*1024)
	var lenBuf [4]byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(lenBuf[:], uint32(n))
			if _, werr := w.Write(lenBuf[:]); werr != nil {
				return werr
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			binary.BigEndian.PutUint32(lenBuf[:], 0)
			_, werr := w.Write(lenBuf[:])
			return werr
		}
		if err != nil {
			return err
		}
	}
}

// ChunkReader 读取长度前缀分块，直到遇到零长度结束块。
type ChunkReader struct {
	r       io.Reader
	pending []byte
	done    bool
	err     error
}

// NewChunkReader 包装底层 Reader 为分块读取器。
func NewChunkReader(r io.Reader) *ChunkReader {
	return &ChunkReader{r: r}
}

// Read 实现 io.Reader，按需拉取下一个分块。
func (c *ChunkReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	if c.done && len(c.pending) == 0 {
		return 0, io.EOF
	}
	if len(c.pending) == 0 {
		var lenBuf [4]byte
		if _, err := io.ReadFull(c.r, lenBuf[:]); err != nil {
			c.err = err
			return 0, err
		}
		n := binary.BigEndian.Uint32(lenBuf[:])
		if n == 0 {
			c.done = true
			return 0, io.EOF
		}
		if n > maxChunk {
			c.err = fmt.Errorf("chunk too large: %d", n)
			return 0, c.err
		}
		c.pending = make([]byte, n)
		if _, err := io.ReadFull(c.r, c.pending); err != nil {
			c.err = err
			return 0, err
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// HeaderFromHTTP 深拷贝 http.Header 为可 JSON 序列化的 map。
func HeaderFromHTTP(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// ApplyHeader 用 src 覆盖写入 dst（先 Del 再 Add）。
func ApplyHeader(dst http.Header, src map[string][]string) {
	for k, vals := range src {
		dst.Del(k)
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// SmuxConfig 返回 Master/Agent 共用的 smux 配置，保证 KeepAlive 参数一致。
func SmuxConfig() *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.KeepAliveTimeout = 60 * time.Second
	return cfg
}
