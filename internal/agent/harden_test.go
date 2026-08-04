package agent

import "testing"

func TestPortFromPanelURL(t *testing.T) {
	got, err := portFromPanelURL("http://127.0.0.1:52045")
	if err != nil || got != 52045 {
		t.Fatalf("got %d err=%v", got, err)
	}
	got, err = portFromPanelURL("https://127.0.0.1:62045/tomas")
	if err != nil || got != 62045 {
		t.Fatalf("got %d err=%v", got, err)
	}
	if _, err := portFromPanelURL("bad"); err == nil {
		t.Fatal("want error for bad url")
	}
	if _, err := portFromPanelURL("http://127.0.0.1"); err == nil {
		t.Fatal("want error for missing port")
	}
}

func TestListensExternallyParse(t *testing.T) {
	if listensExternally(0) {
		t.Fatal("port 0")
	}
}
