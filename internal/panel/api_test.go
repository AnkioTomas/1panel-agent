package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickUpgradeVersion(t *testing.T) {
	if got := PickUpgradeVersion(UpgradeInfo{LatestVersion: "v2.2.5"}); got != "v2.2.5" {
		t.Fatalf("got %q", got)
	}
	if got := PickUpgradeVersion(UpgradeInfo{NewVersion: "v2.2.6", LatestVersion: "v2.2.5"}); got != "v2.2.6" {
		t.Fatalf("prefer newVersion, got %q", got)
	}
	if got := PickUpgradeVersion(UpgradeInfo{}); got != "" {
		t.Fatalf("empty got %q", got)
	}
}

func TestUpdateSSLAndUpgradeAgainstMock(t *testing.T) {
	var sawSSL, sawUpgradeGET, sawUpgradePOST bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/core/settings/ssl/update", func(w http.ResponseWriter, r *http.Request) {
		sawSSL = true
		if r.Header.Get("X-CSRF-Token") != "csrf" {
			t.Fatalf("csrf=%q", r.Header.Get("X-CSRF-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"success","data":null}`))
	})
	mux.HandleFunc("/api/v2/core/settings/upgrade", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			sawUpgradeGET = true
			_, _ = w.Write([]byte(`{"code":200,"message":"","data":{"latestVersion":"","newVersion":""}}`))
			return
		}
		sawUpgradePOST = true
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cookies := []*http.Cookie{{Name: "psession", Value: "s"}, {Name: "pcsrftoken", Value: "csrf"}}
	client := srv.Client()
	if err := UpdateSSL(client, srv.URL, "tomas", cookies, true, "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if !sawSSL {
		t.Fatal("ssl not called")
	}
	res, err := Upgrade(client, srv.URL, "tomas", cookies, "")
	if err != nil {
		t.Fatal(err)
	}
	if !sawUpgradeGET || !res.Skipped || sawUpgradePOST {
		t.Fatalf("upgrade skip path: get=%v post=%v skipped=%v", sawUpgradeGET, sawUpgradePOST, res.Skipped)
	}

	sawUpgradeGET, sawUpgradePOST = false, false
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/api/v2/core/settings/upgrade", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			sawUpgradeGET = true
			_, _ = w.Write([]byte(`{"code":200,"data":{"newVersion":"v9.9.9"}}`))
			return
		}
		sawUpgradePOST = true
		_, _ = w.Write([]byte(`{"code":200}`))
	})
	srv2 := httptest.NewServer(mux2)
	t.Cleanup(srv2.Close)
	res2, err := Upgrade(srv2.Client(), srv2.URL, "", cookies, "")
	if err != nil {
		t.Fatal(err)
	}
	if !sawUpgradeGET || !sawUpgradePOST || res2.Skipped || res2.TargetVersion != "v9.9.9" {
		t.Fatalf("upgrade run: %+v get=%v post=%v", res2, sawUpgradeGET, sawUpgradePOST)
	}
}

func TestAlignCSRF(t *testing.T) {
	h := http.Header{}
	h.Set("Cookie", "a=1; pcsrftoken=tok; b=2")
	AlignCSRF(h)
	if h.Get("X-CSRF-Token") != "tok" {
		t.Fatalf("got %q", h.Get("X-CSRF-Token"))
	}
	h2 := http.Header{}
	AlignCSRF(h2)
	if h2.Get("X-CSRF-Token") != "" {
		t.Fatal("expected empty")
	}
	_ = strings.Builder{}
}
