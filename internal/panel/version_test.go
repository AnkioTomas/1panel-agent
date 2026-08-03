package panel

import "testing"

func TestVersionLineRe(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"版本: v2.2.4\n模式: stable\n", "v2.2.4"},
		{"版本: 2.1.0\n", "2.1.0"},
		{"Version: v2.2.4\nMode: stable\n", "v2.2.4"},
		{"version: v2.2.4\nmode: stable\n", "v2.2.4"}, // 1panel -l en
	}
	for _, tc := range cases {
		m := versionLineRe.FindSubmatch([]byte(tc.in))
		if len(m) != 2 || string(m[1]) != tc.want {
			t.Fatalf("in %q got %q want %q", tc.in, m, tc.want)
		}
	}
}
