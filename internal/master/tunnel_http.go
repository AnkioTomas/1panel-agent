package master

import (
	"bytes"
	"io"
	"net/http"

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

	respMeta := &protocol.ResponseMeta{}
	if err := protocol.ReadJSON(stream, respMeta); err != nil {
		_ = stream.Close()
		return nil, err
	}
	payload, err := io.ReadAll(protocol.NewChunkReader(stream))
	_ = stream.Close()
	if err != nil {
		return nil, err
	}

	status := respMeta.Status
	if status == 0 {
		status = 200
	}
	resp := &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
		Request:       req,
	}
	for k, vals := range respMeta.Headers {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Length" || ck == "Transfer-Encoding" {
			continue
		}
		resp.Header[ck] = vals
	}
	return resp, nil
}
