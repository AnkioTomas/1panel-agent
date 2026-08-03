package agent

import "testing"

func TestPickUpgradeVersion(t *testing.T) {
	if v := pickUpgradeVersion(&panelUpgradeInfo{NewVersion: "v2.1.0", LatestVersion: "v2.2.0"}); v != "v2.1.0" {
		t.Fatalf("prefer newVersion, got %q", v)
	}
	if v := pickUpgradeVersion(&panelUpgradeInfo{LatestVersion: "v2.2.0"}); v != "v2.2.0" {
		t.Fatalf("got %q", v)
	}
	if v := pickUpgradeVersion(&panelUpgradeInfo{TestVersion: "v2.3.0-beta"}); v != "v2.3.0-beta" {
		t.Fatalf("got %q", v)
	}
	if v := pickUpgradeVersion(&panelUpgradeInfo{}); v != "" {
		t.Fatalf("empty want empty, got %q", v)
	}
}
