package master

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"1panel-agent/internal/protocol"

	"github.com/xtaci/smux"
)

func TestSignalAgentUpdateEmptyBody(t *testing.T) {
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})

	var gotLen int64
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
		gotLen, err = io.Copy(io.Discard, protocol.NewChunkReader(stream))
		if err != nil {
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

	if err := signalAgentUpdate(&Session{Mux: mux}); err != nil {
		t.Fatalf("signal: %v", err)
	}
	wg.Wait()
	if gotLen != 0 {
		t.Fatalf("expected empty body, got %d bytes", gotLen)
	}
}

func TestSignalAgentUpdateAgentError(t *testing.T) {
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

	err = signalAgentUpdate(&Session{Mux: mux})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err=%v", err)
	}
}

func TestHandleForceUpdateAsync(t *testing.T) {
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
		_ = protocol.WriteJSON(stream, &protocol.ResponseMeta{Status: http.StatusOK})
		_ = protocol.CopyChunks(stream, bytes.NewReader([]byte(`{"ok":true}`)))
	}()
	mux, err := smux.Client(c1, protocol.SmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Close() })

	s := &Server{reg: NewRegistry()}
	s.reg.Put(&Session{Info: AgentInfo{ID: "a1", Name: "n1"}, Mux: mux})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__mp/api/force-update", nil)
	s.handleForceUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var accepted map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted["accepted"] != true {
		t.Fatalf("body=%v", accepted)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec = httptest.NewRecorder()
		s.handleForceUpdate(rec, httptest.NewRequest(http.MethodGet, "/__mp/api/force-update", nil))
		var st forceUpdateStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		if !st.Running {
			if st.OK != 1 {
				t.Fatalf("status=%+v", st)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("force-update still running")
}
