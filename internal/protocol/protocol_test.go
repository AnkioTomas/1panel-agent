package protocol

import (
	"bytes"
	"io"
	"testing"
)

func TestJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Register{ID: "abc", Hostname: "host1", PanelURL: "http://127.0.0.1:1"}
	if err := WriteJSON(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out Register
	if err := ReadJSON(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestRequestMetaRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := &RequestMeta{
		Type:   StreamTypeHTTP,
		Method: "POST",
		Path:   "/api/x?y=1",
		Headers: map[string][]string{
			"X-Test": {"a", "b"},
		},
	}
	if err := WriteRequestMeta(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadRequestMeta(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != StreamTypeHTTP || out.Method != "POST" || out.Path != "/api/x?y=1" {
		t.Fatalf("unexpected meta: %+v", out)
	}
	if got := out.Headers["X-Test"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("headers: %v", got)
	}
}

func TestChunksRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := bytes.Repeat([]byte("0123456789"), 4000)
	if err := CopyChunks(&buf, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(NewChunkReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("len got=%d want=%d", len(got), len(payload))
	}
}
