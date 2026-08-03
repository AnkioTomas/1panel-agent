package master

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"1panel-agent/internal/panel"
)

// upgradeCheckItem 是单个节点（Master 或 Agent）的版本检查结果。
type upgradeCheckItem struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Status  string `json:"status"` // latest | outdated | unknown
	Latest  string `json:"latest,omitempty"`
}

// upgradeCheckResult 是 /api/upgrade-check 的完整响应。
type upgradeCheckResult struct {
	MasterVersion string             `json:"master_version"`
	Latest        string             `json:"latest"`
	MasterStatus  string             `json:"master_status"`
	Agents        []upgradeCheckItem `json:"agents"`
	Message       string             `json:"message,omitempty"`
}

// panelUpgradeInfo 对应本机 1Panel upgrade API 的 data 字段。
type panelUpgradeInfo struct {
	TestVersion   string `json:"testVersion"`
	NewVersion    string `json:"newVersion"`
	LatestVersion string `json:"latestVersion"`
}

// handleUpgradeCheck 查询本机与在线 Agent 相对最新版的状态。
func (s *Server) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	masterVer := panel.ReadSystemVersion()
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

	latest, err := s.fetchPanelLatestVersion(r)
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

// fetchPanelLatestVersion 复用浏览器本机 1Panel 会话，读取官方最新版本号。
func (s *Server) fetchPanelLatestVersion(r *http.Request) (string, error) {
	if s.LocalPanel == "" {
		return "", fmt.Errorf("local panel not configured")
	}
	// Reuse the caller's 1Panel session — no master-stored password.
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(s.LocalPanel, "/")+"/api/v2/core/settings/upgrade", nil)
	if err != nil {
		return "", err
	}
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if s.Entrance != "" {
		req.Header.Set("EntranceCode", base64.StdEncoding.EncodeToString([]byte(s.Entrance)))
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
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

// statusKnownLatest 在无最新候选时：有版本视为 latest，否则 unknown。
func statusKnownLatest(current string) string {
	if strings.TrimSpace(current) == "" {
		return "unknown"
	}
	return "latest"
}

// statusFor 比较 current 与 latest，返回 latest / outdated / unknown。
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

// comparePanelVersion 比较版本号：a<b 返回 -1，相等 0，a>b 返回 1。
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
	n := max(len(bs), len(as))
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

// normalizeVer 去掉空白与前导 v，便于字符串比较回退。
func normalizeVer(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "v")
	return s
}

// versionParts 将版本拆成整数段；无法解析时返回 nil。
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

// firstNonEmpty 返回第一个非空白字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// truncateStr 截断过长字符串，用于错误日志。
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
