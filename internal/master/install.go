package master

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"text/template"
)

func (s *Server) requireInstallToken(w http.ResponseWriter, r *http.Request) bool {
	tok := r.URL.Query().Get("token")
	if tok == "" || tok != s.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) handleAgentScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireInstallToken(w, r) {
		return
	}
	host := s.AdvertiseHost(r)
	base := "http://" + host
	data := struct {
		Base   string
		Master string
		Token  string
		GOOS   string
		GOARCH string
	}{
		Base:   base,
		Master: host,
		Token:  s.Token,
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

func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireInstallToken(w, r) {
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

// InstallCommand returns the one-liner shown in Master UI.
func (s *Server) InstallCommand(r *http.Request) string {
	host := s.AdvertiseHost(r)
	return fmt.Sprintf(`curl -fsSL "http://%s/agent.sh?token=%s" | sudo bash`, host, s.Token)
}

var agentInstallTmpl = template.Must(template.New("agent.sh").Parse(strings.TrimSpace(`
#!/bin/bash
# 1pm agent bootstrap — idempotent reinstall from Master
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

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
echo ">> downloading 1pm from ${BASE}"
curl -fsSL "${BASE}/agent.bin?token=${TOKEN}" -o "$TMP"
chmod 755 "$TMP"

systemctl stop 1pm-agent.service 2>/dev/null || true
install -m 755 "$TMP" "$BIN_PATH"

cat > "$UNIT_PATH" <<EOF
[Unit]
Description=1Panel multi-node agent tunnel
After=network-online.target 1panel-core.service
Wants=network-online.target

[Service]
Type=simple
Environment=HOME=/root
ExecStart=${BIN_PATH} agent register ${MASTER}/${TOKEN}
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
echo ">> re-run this curl anytime to reset/reinstall"
`) + "\n"))
