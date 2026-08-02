package master

import "testing"

func TestComparePanelVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.2.4", "v2.2.4", 0},
		{"2.2.4", "v2.2.5", -1},
		{"v2.3.0", "v2.2.9", 1},
		{"v2.2.4", "v2.10.0", -1},
	}
	for _, tc := range cases {
		got := comparePanelVersion(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("compare(%s,%s)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestStatusFor(t *testing.T) {
	if statusFor("v2.2.4", "v2.2.5") != "outdated" {
		t.Fatal("expected outdated")
	}
	if statusFor("v2.2.5", "v2.2.5") != "latest" {
		t.Fatal("expected latest")
	}
	if statusFor("", "v2.2.5") != "unknown" {
		t.Fatal("expected unknown")
	}
}
