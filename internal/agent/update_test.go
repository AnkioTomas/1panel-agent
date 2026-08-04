package agent

import (
	"bytes"
	"testing"

	"1panel-agent/internal/config"
)

func TestReplaceBinaryFromEmptyFallsBackToHTTP(t *testing.T) {
	c := &Client{Cfg: &config.Agent{}}
	err := c.replaceBinaryFrom(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "master/token missing" {
		t.Fatalf("err=%v", err)
	}
}
