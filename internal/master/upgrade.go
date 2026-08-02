package master

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"1panel-agent/internal/panel"
)

type upgradeCheckItem struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Status  string `json:"status"` // latest | outdated | unknown
	Latest  string `json:"latest,omitempty"`
}

type upgradeCheckResult struct {
	MasterVersion string             `json:"master_version"`
	Latest        string             `json:"latest"`
	MasterStatus  string             `json:"master_status"`
	Agents        []upgradeCheckItem `json:"agents"`
	Message       string             `json:"message,omitempty"`
}

type panelUpgradeInfo struct {
	TestVersion   string `json:"testVersion"`
	NewVersion    string `json:"newVersion"`
	LatestVersion string `json:"latestVersion"`
}

func (s *Server) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	masterVer := panel.ReadSystemVersion("")
	agents := s.reg.List()
	out := upgradeCheckResult{
		MasterVersion: masterVer,
		Agents:        make([]upgradeCheckItem, 0, len(agents)),
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, upgradeCheckItem{
			ID:      a.ID,
			Version: a.PanelVersion,
			Status:  "unknown",
		})
	}

	latest, err := s.fetchPanelLatestVersion()
	if err != nil {
		out.Message = err.Error()
		out.MasterStatus = "unknown"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	out.Latest = latest
	if latest == "" {
		// API ok but no candidate — treat known versions as up to date.
		out.MasterStatus = statusKnownLatest(masterVer)
		for i := range out.Agents {
			out.Agents[i].Status = statusKnownLatest(out.Agents[i].Version)
		}
	} else {
		out.MasterStatus = statusFor(masterVer, latest)
		for i := range out.Agents {
			out.Agents[i].Status = statusFor(out.Agents[i].Version, latest)
			if out.Agents[i].Status == "outdated" {
				out.Agents[i].Latest = latest
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) fetchPanelLatestVersion() (string, error) {
	if s.LocalPanel == "" {
		return "", fmt.Errorf("local panel not configured")
	}
	if s.PanelUser == "" || s.PanelPass == "" {
		return "", fmt.Errorf("panel user/password not configured")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := panel.LoginWithClient(client, s.LocalPanel, s.Entrance, s.PanelUser, s.PanelPass, "")
	if err != nil {
		return "", fmt.Errorf("login local panel: %w", err)
	}
	base, _ := url.Parse(strings.TrimRight(s.LocalPanel, "/"))
	jar.SetCookies(base, res.Cookies)

	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(s.LocalPanel, "/")+"/api/v2/core/settings/upgrade", nil)
	if err != nil {
		return "", err
	}
	if s.Entrance != "" {
		// EntranceCode not always required after session login; keep if panel still checks.
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var ar struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return "", fmt.Errorf("upgrade decode: %w body=%s", err, truncateStr(string(raw), 200))
	}
	if ar.Code != 200 {
		return "", fmt.Errorf("upgrade api: %s", ar.Message)
	}
	var info panelUpgradeInfo
	if len(ar.Data) > 0 && string(ar.Data) != "null" {
		_ = json.Unmarshal(ar.Data, &info)
	}
	latest := firstNonEmpty(info.NewVersion, info.LatestVersion, info.TestVersion)
	return latest, nil
}

func statusKnownLatest(current string) string {
	if strings.TrimSpace(current) == "" {
		return "unknown"
	}
	return "latest"
}

func statusFor(current, latest string) string {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	if current == "" {
		return "unknown"
	}
	if latest == "" {
		return "unknown"
	}
	cmp := comparePanelVersion(current, latest)
	if cmp >= 0 {
		return "latest"
	}
	return "outdated"
}

// comparePanelVersion returns -1 if a<b, 0 if equal, 1 if a>b.
func comparePanelVersion(a, b string) int {
	as := versionParts(a)
	bs := versionParts(b)
	if len(as) == 0 || len(bs) == 0 {
		if normalizeVer(a) == normalizeVer(b) {
			return 0
		}
		if normalizeVer(a) < normalizeVer(b) {
			return -1
		}
		return 1
	}
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai = as[i]
		}
		if i < len(bs) {
			bi = bs[i]
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

func normalizeVer(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "v")
	return s
}

func versionParts(s string) []int {
	s = normalizeVer(s)
	if s == "" {
		return nil
	}
	// strip pre-release suffix: 2.2.4-beta -> 2.2.4
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	segs := strings.Split(s, ".")
	out := make([]int, 0, len(segs))
	for _, seg := range segs {
		n, err := strconv.Atoi(seg)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
