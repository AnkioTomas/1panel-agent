package master

import "testing"

func TestRewriteLocation(t *testing.T) {
	prefix := "/n/abc"
	tests := []struct {
		in, want string
	}{
		{"/login", "/n/abc/login"},
		{"http://127.0.0.1:20560/x", "/n/abc/x"},
		{"relative", "relative"},
	}
	for _, tt := range tests {
		if got := rewriteLocation(tt.in, prefix); got != tt.want {
			t.Fatalf("%q: got %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestRewriteSetCookie(t *testing.T) {
	got := rewriteSetCookie("p_token=1; Path=/; HttpOnly", "/n/abc")
	if got != "p_token=1; Path=/n/abc/; HttpOnly" {
		t.Fatalf("got %q", got)
	}
}
