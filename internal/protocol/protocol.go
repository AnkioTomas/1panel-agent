package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	StreamTypeHTTP byte = 1
	StreamTypeWS   byte = 2
	maxJSONFrame        = 16 << 20
	maxChunk            = 8 << 20
)

// Register is the first JSON message Agent sends after WebSocket upgrade.
type Register struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	PanelURL string `json:"panel_url"`
}

// RegisterOK is Master's acknowledgment before smux takes over.
type RegisterOK struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RequestMeta is written at the start of each smux stream from Master to Agent.
type RequestMeta struct {
	Type    byte                `json:"-"`
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
}

// ResponseMeta is written at the start of each HTTP response stream.
type ResponseMeta struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
}

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

func WriteRequestMeta(w io.Writer, meta *RequestMeta) error {
	if meta.Type == 0 {
		meta.Type = StreamTypeHTTP
	}
	if _, err := w.Write([]byte{meta.Type}); err != nil {
		return err
	}
	return WriteJSON(w, meta)
}

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

func WriteResponseMeta(w io.Writer, meta *ResponseMeta) error {
	return WriteJSON(w, meta)
}

func ReadResponseMeta(r io.Reader) (*ResponseMeta, error) {
	meta := &ResponseMeta{}
	if err := ReadJSON(r, meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// CopyChunks writes r to w as length-prefixed chunks, ending with a zero chunk.
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

// ChunkReader reads length-prefixed chunks until a zero chunk.
type ChunkReader struct {
	r       io.Reader
	pending []byte
	done    bool
	err     error
}

func NewChunkReader(r io.Reader) *ChunkReader {
	return &ChunkReader{r: r}
}

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

func HeaderFromHTTP(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func ApplyHeader(dst http.Header, src map[string][]string) {
	for k, vals := range src {
		dst.Del(k)
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
