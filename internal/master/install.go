package master

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"1panel-agent/internal/config"
)

// authorizeAgentDownload 校验 GET/HEAD，并用 timestamp+sign（与 /agent/ws 相同）鉴权。
func (s *Server) authorizeAgentDownload(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !s.VerifyToken(r.URL.Query().Get("timestamp"), r.URL.Query().Get("sign")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// handleAgentScript 在签名校验通过后下发 Agent 安装脚本（/agent.sh）。
func (s *Server) handleAgentScript(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgentDownload(w, r) {
		return
	}
	host := s.AdvertiseHost(r)
	data := struct {
		Base   string
		Master string
		Token  string
		GOOS   string
		GOARCH string
	}{
		Base:   "http://" + host,
		Master: host,
		Token:  s.currentToken(),
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_ = agentInstallTmpl.Execute(w, data)
}

// handleAgentBinary 在签名校验通过后下发当前 Master 二进制作为 Agent（/agent.bin）。
func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgentDownload(w, r) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		http.Error(w, "executable path unavailable", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(exe)
	if err != nil {
		http.Error(w, "open binary: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "stat binary: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="1pm"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-1pm-GOOS", runtime.GOOS)
	w.Header().Set("X-1pm-GOARCH", runtime.GOARCH)
	http.ServeContent(w, r, "1pm", st.ModTime(), f)
}

// InstallCommand 返回管理页展示的一键安装命令（timestamp+sign，约 5 分钟有效）。
func (s *Server) InstallCommand(r *http.Request) string {
	host := s.AdvertiseHost(r)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign(s.currentToken(), ts)
	return fmt.Sprintf(`curl -fsSL "http://%s/agent.sh?timestamp=%s&sign=%s" | sudo bash`, host, ts, sign)
}

// agentInstallTmpl 是 /agent.sh 安装脚本模板。
// 安装时：签名下载二进制 → agent install 落盘 → systemd 仅执行 agent run。
var agentInstallTmpl = template.Must(template.New("agent.sh").Parse(strings.TrimSpace(`
#!/bin/bash
# 1pm agent bootstrap — install-time config, runtime is "agent run"
set -euo pipefail

MASTER={{printf "%q" .Master}}
TOKEN={{printf "%q" .Token}}
BASE={{printf "%q" .Base}}
EXPECT_GOOS={{printf "%q" .GOOS}}
EXPECT_GOARCH={{printf "%q" .GOARCH}}
BIN_PATH=/usr/local/bin/1pm
UNIT_PATH=/etc/systemd/system/1pm-agent.service

if [[ "$(id -u)" -ne 0 ]]; then
  echo "error: run as root (use: curl ... | sudo bash)" >&2
  exit 1
fi

uname_m="$(uname -m)"
case "$uname_m" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "error: unsupported arch: $uname_m" >&2; exit 1 ;;
esac
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$OS" != "$EXPECT_GOOS" || "$ARCH" != "$EXPECT_GOARCH" ]]; then
  echo "warn: master binary is ${EXPECT_GOOS}/${EXPECT_GOARCH}, this host is ${OS}/${ARCH}" >&2
  echo "warn: continuing anyway; rebuild Master for this arch if the binary fails to run" >&2
fi

# 与 Master VerifyToken 相同的 HMAC-SHA256(timestamp=<ts>)
sign_query() {
  local ts sign
  ts="$(date +%s)"
  sign="$(printf 'timestamp=%s' "$ts" | openssl dgst -sha256 -hmac "$TOKEN" 2>/dev/null | awk '{print $NF}')"
  if [[ -z "$sign" ]]; then
    echo "error: openssl is required to sign download requests" >&2
    exit 1
  fi
  printf 'timestamp=%s&sign=%s' "$ts" "$sign"
}

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
echo ">> downloading 1pm from ${BASE}"
curl -fsSL "${BASE}/agent.bin?$(sign_query)" -o "$TMP"
chmod 755 "$TMP"

systemctl stop 1pm-agent.service 2>/dev/null || true
install -m 755 "$TMP" "$BIN_PATH"

echo ">> writing agent config (install-time registration)"
"$BIN_PATH" agent install "$MASTER" "$TOKEN"

if [[ -t 0 ]]; then
  echo -n "1Panel password for auto-login (Enter to skip): "
  read -rs PANEL_PASS || true
  echo
  if [[ -n "${PANEL_PASS:-}" ]]; then
    "$BIN_PATH" agent setpwd --password "$PANEL_PASS"
  fi
elif [[ -n "${PANEL_PASS:-}" ]]; then
  "$BIN_PATH" agent setpwd --password "$PANEL_PASS"
fi

cat > "$UNIT_PATH" <<EOF
[Unit]
Description=1Panel multi-node agent tunnel
After=network-online.target 1panel-core.service
Wants=network-online.target

[Service]
Type=simple
Environment=HOME=/root
ExecStart=${BIN_PATH} agent run
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable 1pm-agent.service
systemctl restart 1pm-agent.service
systemctl --no-pager --full status 1pm-agent.service || true
echo ">> 1pm agent installed and started (master=${MASTER})"
echo ">> re-run this curl anytime to reset/reinstall (signed URL expires ~5 minutes)"
`) + "\n"))
