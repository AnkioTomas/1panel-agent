package agent

import "testing"

func TestPortFromPanelURL(t *testing.T) {
	if got := portFromPanelURL("http://127.0.0.1:52045"); got != 52045 {
		t.Fatalf("got %d", got)
	}
	if got := portFromPanelURL("https://127.0.0.1:62045/tomas"); got != 62045 {
		t.Fatalf("got %d", got)
	}
	if got := portFromPanelURL("bad"); got != 52045 {
		t.Fatalf("default got %d", got)
	}
}

func TestListensExternallyParse(t *testing.T) {
	// 无端口时不应误判
	if listensExternally(0) {
		t.Fatal("port 0")
	}
}
