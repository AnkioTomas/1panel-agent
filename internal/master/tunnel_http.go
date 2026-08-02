package master

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

// tunnelTransport sends HTTP requests through an agent smux session.
type tunnelTransport struct {
	mux *smux.Session
}

func (t *tunnelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	stream, err := t.mux.OpenStream()
	if err != nil {
		return nil, err
	}

	path := req.URL.RequestURI()
	if path == "" {
		path = "/"
	}
	headers := protocol.HeaderFromHTTP(req.Header)
	delete(headers, "Host")
	meta := &protocol.RequestMeta{
		Type:    protocol.StreamTypeHTTP,
		Method:  req.Method,
		Path:    path,
		Headers: headers,
	}
	if err := protocol.WriteRequestMeta(stream, meta); err != nil {
		_ = stream.Close()
		return nil, err
	}
	body := io.Reader(http.NoBody)
	if req.Body != nil {
		body = req.Body
	}
	if err := protocol.CopyChunks(stream, body); err != nil {
		_ = stream.Close()
		return nil, err
	}

	respMeta, err := protocol.ReadResponseMeta(stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	payload, err := io.ReadAll(protocol.NewChunkReader(stream))
	_ = stream.Close()
	if err != nil {
		return nil, err
	}

	raw := buildHTTPResponse(respMeta.Status, respMeta.Headers, payload)
	return http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), req)
}

func buildHTTPResponse(status int, headers map[string][]string, body []byte) []byte {
	if status == 0 {
		status = 200
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "HTTP/1.1 %d %s\r\n", status, http.StatusText(status))
	for k, vals := range headers {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Length" || ck == "Transfer-Encoding" {
			continue
		}
		for _, v := range vals {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(&b, "Content-Length: %s\r\n\r\n", strconv.Itoa(len(body)))
	b.Write(body)
	return b.Bytes()
}
