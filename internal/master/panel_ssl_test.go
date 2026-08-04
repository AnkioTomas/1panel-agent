package master

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"1panel-agent/internal/protocol"
)

func TestHostOnly(t *testing.T) {
	if got := hostOnly("10.211.55.14:52045"); got != "10.211.55.14" {
		t.Fatalf("got %q", got)
	}
	if got := hostOnly("example.com"); got != "example.com" {
		t.Fatalf("got %q", got)
	}
	if got := hostOnly(""); got != "127.0.0.1" {
		t.Fatalf("empty got %q", got)
	}
}

func TestHandlePanelSSLMethod(t *testing.T) {
	s := &Server{reg: NewRegistry()}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__mp/api/panel-ssl", nil)
	s.handlePanelSSL(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestHandleUpgradePanelMasterMethod(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__mp/api/upgrade-panel-master", nil)
	s.handleUpgradePanelMaster(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestPanelControlJSON(t *testing.T) {
	raw, _ := json.Marshal(protocol.PanelControl{Action: "master_tls", Enable: true})
	var got protocol.PanelControl
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "master_tls" || !got.Enable {
		t.Fatalf("%+v", got)
	}
	_ = bytes.NewReader(raw)
}

func TestCountOK(t *testing.T) {
	if n := countOK(nil); n != 0 {
		t.Fatal(n)
	}
	if n := countOK([]panelSSLResult{{OK: true}, {OK: false}, {OK: true}}); n != 2 {
		t.Fatal(n)
	}
}
