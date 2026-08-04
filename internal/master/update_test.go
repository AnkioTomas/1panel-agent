package master

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

func TestPushAgentUpdateSendsBinaryOverTunnel(t *testing.T) {
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})

	payload := bytes.Repeat([]byte("1pm-bin-"), 4096) // ~32KiB
	var got bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv, err := smux.Server(c2, protocol.SmuxConfig())
		if err != nil {
			t.Errorf("smux server: %v", err)
			return
		}
		defer srv.Close()
		stream, err := srv.AcceptStream()
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer stream.Close()
		meta, err := protocol.ReadRequestMeta(stream)
		if err != nil {
			t.Errorf("meta: %v", err)
			return
		}
		if meta.Type != protocol.StreamTypeUpdate {
			t.Errorf("type=%d", meta.Type)
		}
		body := protocol.NewChunkReader(stream)
		if _, err := io.Copy(&got, body); err != nil {
			t.Errorf("copy: %v", err)
			return
		}
		_ = protocol.WriteJSON(stream, &protocol.ResponseMeta{Status: http.StatusOK})
		_ = protocol.CopyChunks(stream, bytes.NewReader([]byte(`{"ok":true}`)))
	}()

	mux, err := smux.Client(c1, protocol.SmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Close() })

	err = pushAgentUpdate(&Session{Mux: mux}, int64(len(payload)), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	wg.Wait()
	if !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("payload mismatch: got %d want %d", got.Len(), len(payload))
	}
}

func TestPushAgentUpdateAgentError(t *testing.T) {
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	go func() {
		srv, err := smux.Server(c2, protocol.SmuxConfig())
		if err != nil {
			return
		}
		defer srv.Close()
		stream, err := srv.AcceptStream()
		if err != nil {
			return
		}
		defer stream.Close()
		_, _ = protocol.ReadRequestMeta(stream)
		_, _ = io.Copy(io.Discard, protocol.NewChunkReader(stream))
		_ = protocol.WriteJSON(stream, &protocol.ResponseMeta{Status: http.StatusBadGateway})
		_ = protocol.CopyChunks(stream, bytes.NewReader([]byte("boom")))
	}()

	mux, err := smux.Client(c1, protocol.SmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Close() })

	err = pushAgentUpdate(&Session{Mux: mux}, 4, func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte("abcd"))), nil
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err=%v", err)
	}
}
