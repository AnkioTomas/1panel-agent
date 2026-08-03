package agent

import "testing"

func TestParseInstallTarget(t *testing.T) {
	tests := []struct {
		in         string
		wantMaster string
		wantToken  string
		wantErr    bool
	}{
		{"1.2.3.4:8080/secret", "1.2.3.4:8080", "secret", false},
		{"http://host:9/tok", "host:9", "tok", false},
		{"ws://host:9/a/b", "host:9/a", "b", false},
		{"host/token", "", "", true},
		{"host:9/", "", "", true},
		{"/token", "", "", true},
	}
	for _, tt := range tests {
		m, tok, err := ParseInstallTarget(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tt.in, err)
		}
		if m != tt.wantMaster || tok != tt.wantToken {
			t.Fatalf("%q: got %s %s", tt.in, m, tok)
		}
	}
}
