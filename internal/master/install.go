package master

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"1panel-agent/internal/buildinfo"
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
// 脚本只从 Master 取 Token/入口；二进制从 Release/CDN 下载（不走 /agent.bin）。
func (s *Server) handleAgentScript(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAgentDownload(w, r) {
		return
	}
	host := s.AdvertiseHost(r)
	scheme := s.PublicScheme()
	rel := releaseConfigFromState()
	version := ""
	if strings.HasPrefix(buildinfo.Version, "v") {
		version = buildinfo.Version
	}
	data := struct {
		Base       string
		Master     string
		Token      string
		MasterTLS  bool
		Name       string
		Group      string
		Repo       string
		GitHubAPI  string
		GitHubDL   string
		InstallCDN string
		Version    string
	}{
		Base:       scheme + "://" + host,
		Master:     host,
		Token:      s.currentToken(),
		MasterTLS:  scheme == "https",
		Name:       config.SanitizeMeta(r.URL.Query().Get("name")),
		Group:      config.SanitizeMeta(r.URL.Query().Get("group")),
		Repo:       rel.Repo,
		GitHubAPI:  rel.GitHubAPI,
		GitHubDL:   rel.GitHubDL,
		InstallCDN: rel.InstallCDN,
		Version:    version,
	}
	if data.Repo == "" {
		data.Repo = "AnkioTomas/1panel-agent"
	}
	if data.GitHubAPI == "" {
		data.GitHubAPI = "https://api.github.com"
	}
	if data.GitHubDL == "" {
		data.GitHubDL = "https://github.com"
	}
	if data.InstallCDN == "" {
		data.InstallCDN = "auto"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_ = agentInstallTmpl.Execute(w, data)
}

// handleAgentBinary 在签名校验通过后下发当前 Master 二进制作为 Agent（/agent.bin）。
// 保留兼容；新版 agent.sh 默认走 CDN，仅作备用。
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
// name/group 来自请求 query，写入 agent.sh URL，安装时落盘并随注册上报。
func (s *Server) InstallCommand(r *http.Request) string {
	host := s.AdvertiseHost(r)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := config.Sign(s.currentToken(), ts)
	q := url.Values{}
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	if name := config.SanitizeMeta(r.URL.Query().Get("name")); name != "" {
		q.Set("name", name)
	}
	if group := config.SanitizeMeta(r.URL.Query().Get("group")); group != "" {
		q.Set("group", group)
	}
	curlFlags := "-fsSL"
	if s.PublicScheme() == "https" {
		curlFlags = "-fsSLk" // 面板自签常见
	}
	return fmt.Sprintf(`curl %s "%s://%s/agent.sh?%s" | sudo bash`, curlFlags, s.PublicScheme(), host, q.Encode())
}

// handleInstallCommand 实时生成带签名的安装命令（复制前调用，避免签名过期）。
func (s *Server) handleInstallCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"install": s.InstallCommand(r),
	})
}

// agentInstallTmpl 是 /agent.sh 安装脚本模板。
// Master 只提供鉴权脚本与注册参数；1pm 二进制从 Release/CDN 下载。
var agentInstallTmpl = template.Must(template.New("agent.sh").Parse(strings.TrimSpace(`
#!/bin/bash
# 1pm agent bootstrap — binary from Release/CDN; Master only for register config
set -euo pipefail

MASTER={{printf "%q" .Master}}
TOKEN={{printf "%q" .Token}}
BASE={{printf "%q" .Base}}
MASTER_TLS={{if .MasterTLS}}1{{else}}0{{end}}
NODE_NAME={{printf "%q" .Name}}
NODE_GROUP={{printf "%q" .Group}}
REPO={{printf "%q" .Repo}}
GITHUB_API={{printf "%q" .GitHubAPI}}
GITHUB_DL={{printf "%q" .GitHubDL}}
INSTALL_CDN={{printf "%q" .InstallCDN}}
VERSION={{printf "%q" .Version}}
BIN_PATH=/usr/local/bin/1pm
UNIT_PATH=/etc/systemd/system/1pm-agent.service

MIRROR_PREFIXES=(
  "https://gh-proxy.com/"
  "https://ghfast.top/"
  "https://ghproxy.net/"
  "https://cdn.gh-proxy.com/"
  "https://gh.dpik.top/"
  "https://gh.monlor.com/"
  "https://gh.noki.icu/"
  "https://gh.tryxd.cn/"
  "https://ghpr.cc/"
  "https://gitproxy.click/"
)

PREFERRED_MIRROR=""
RESOLVED_TAG=""

log()  { echo "==> $*" >&2; }
warn() { echo "warn: $*" >&2; }
die()  { echo "error: $*" >&2; exit 1; }
have_cmd() { command -v "$1" >/dev/null 2>&1; }

if [[ "$(id -u)" -ne 0 ]]; then
  die "run as root (use: curl ... | sudo bash)"
fi

# 一台机器不能同时是 agent 和 master
if [[ -f /etc/systemd/system/1pm-master.service ]] || [[ -f /var/lib/1pm/master.json ]]; then
  die "本机已安装 1pm master，不能同时作为 agent。先执行: 1pm uninstall"
fi

uname_m="$(uname -m)"
case "$uname_m" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported arch: $uname_m" ;;
esac
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
[[ "$OS" == "linux" ]] || die "agent install supports linux only (got $OS)"
ASSET="1pm_linux_${ARCH}"

mirror_prefixes() {
  local mode="$1" m
  case "$mode" in
    global) printf '%s\n' "" ;;
    cn)
      for m in "${MIRROR_PREFIXES[@]}"; do printf '%s\n' "$m"; done
      printf '%s\n' ""
      ;;
    auto|*)
      [[ -n "$PREFERRED_MIRROR" ]] && printf '%s\n' "$PREFERRED_MIRROR"
      for m in "${MIRROR_PREFIXES[@]}"; do printf '%s\n' "$m"; done
      printf '%s\n' ""
      ;;
  esac
}

http_get() {
  local url="$1" out="$2" code=0
  if have_cmd curl; then
    code="$(curl -L --connect-timeout 15 --max-time 600 --retry 1 --retry-delay 1 \
      -o "$out" -w '%{http_code}' "$url" 2>/dev/null || echo 000)"
    if [[ "$code" =~ ^2[0-9][0-9]$ ]] && [[ -s "$out" ]]; then
      return 0
    fi
    warn "HTTP ${code}: $url"
    rm -f "$out"
    return 1
  fi
  die "need curl"
}

pick_mirror_for_tag() {
  local tag="$1" prefix url tmp
  PREFERRED_MIRROR=""
  [[ "$INSTALL_CDN" == "global" ]] && return 0
  tmp="$(mktemp)"
  log "探测可用下载节点…"
  while IFS= read -r prefix; do
    if [[ -z "$prefix" ]]; then
      url="${GITHUB_DL}/${REPO}/releases/download/${tag}/checksums.txt"
    else
      url="${prefix}${GITHUB_DL}/${REPO}/releases/download/${tag}/checksums.txt"
    fi
    if http_get "$url" "$tmp"; then
      PREFERRED_MIRROR="$prefix"
      if [[ -z "$prefix" ]]; then
        log "下载节点: github.com（直连）"
      else
        log "下载节点: ${prefix#https://}"
      fi
      rm -f "$tmp"
      return 0
    fi
  done < <(mirror_prefixes "$INSTALL_CDN")
  rm -f "$tmp"
  warn "未找到可用下载节点，稍后仍会逐个重试"
}

resolve_version() {
  RESOLVED_TAG=""
  local prefix url tmp tag
  if [[ -n "$VERSION" ]]; then
    RESOLVED_TAG="$VERSION"
    return
  fi
  tmp="$(mktemp)"
  while IFS= read -r prefix; do
    if [[ -z "$prefix" ]]; then
      url="${GITHUB_API}/repos/${REPO}/releases/latest"
    else
      url="${prefix}${GITHUB_API}/repos/${REPO}/releases/latest"
    fi
    if http_get "$url" "$tmp"; then
      tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp" | head -n1)"
      if [[ -n "$tag" ]]; then
        rm -f "$tmp"
        log "latest release: $tag"
        RESOLVED_TAG="$tag"
        return
      fi
    fi
  done < <(mirror_prefixes "$INSTALL_CDN")
  rm -f "$tmp"
  die "cannot resolve latest release"
}

assert_clean_tag() {
  local tag="$1"
  if [[ "$tag" == *$'\n'* || "$tag" == *'==>'* || "$tag" == *' '* ]]; then
    die "invalid release tag: $tag"
  fi
  [[ "$tag" =~ ^v[0-9] ]] || die "invalid release tag: $tag"
}

download_asset() {
  local tag="$1" file="$2" out="$3" required="${4:-1}"
  local prefix url tried="" ok=0 order=()
  order+=("$PREFERRED_MIRROR")
  while IFS= read -r prefix; do
    [[ "$prefix" == "$PREFERRED_MIRROR" ]] && continue
    order+=("$prefix")
  done < <(mirror_prefixes "$INSTALL_CDN")
  for prefix in "${order[@]}"; do
    if [[ -z "$prefix" ]]; then
      url="${GITHUB_DL}/${REPO}/releases/download/${tag}/${file}"
    else
      url="${prefix}${GITHUB_DL}/${REPO}/releases/download/${tag}/${file}"
    fi
    tried="${tried}${tried:+ | }${url}"
    if http_get "$url" "$out"; then
      log "已下载 $file"
      ok=1
      break
    fi
  done
  if [[ "$ok" -eq 1 ]]; then
    return 0
  fi
  if [[ "$required" == "1" ]]; then
    die "download failed: $file"$'\n'"tried: $tried"
  fi
  return 1
}

verify_checksum() {
  local dir="$1" file="$2" sumfile="$3"
  [[ -f "$sumfile" ]] || { warn "no checksums.txt; skip verify"; return 0; }
  if have_cmd sha256sum; then
    local line
    line="$(grep -E "[[:space:]]${file}$" "$sumfile" || true)"
    [[ -n "$line" ]] || { warn "checksum entry missing for $file"; return 0; }
    (cd "$dir" && echo "$line" | sha256sum -c -) || die "checksum mismatch for $file"
  else
    warn "no sha256sum; skip verify"
  fi
}

# curl|bash 时 stdin 是脚本本身，密码必须从 /dev/tty 读。
ask_panel_password() {
  if [[ -n "${PANEL_PASS:-}" ]]; then
    return
  fi
  if [[ ! -r /dev/tty ]]; then
    die "需要本机 1Panel 密码：export PANEL_PASS='...' 后重跑，或在终端交互执行"
  fi
  local p1 p2
  while true; do
    printf '本机 1Panel 密码（远程自动登录用）: ' > /dev/tty
    read -rs p1 < /dev/tty || true
    printf '\n' > /dev/tty
    [[ -n "${p1:-}" ]] || { echo "密码不能为空" > /dev/tty; continue; }
    printf '再输入一次: ' > /dev/tty
    read -rs p2 < /dev/tty || true
    printf '\n' > /dev/tty
    if [[ "$p1" != "$p2" ]]; then
      echo "两次不一致，请重试" > /dev/tty
      continue
    fi
    PANEL_PASS="$p1"
    break
  done
}

save_panel_password() {
  local tries=0 from_env=0
  [[ -n "${PANEL_PASS:-}" ]] && from_env=1
  while true; do
    ask_panel_password
    log "验证并保存面板密码…"
    if "$BIN_PATH" agent setpwd --password "$PANEL_PASS"; then
      return 0
    fi
    if [[ "$from_env" -eq 1 ]]; then
      die "PANEL_PASS 密码验证失败（请检查本机 1Panel 密码）"
    fi
    if [[ -w /dev/tty ]]; then
      echo "密码验证失败，请重试" > /dev/tty
    else
      echo "密码验证失败，请重试" >&2
    fi
    PANEL_PASS=""
    tries=$((tries + 1))
    [[ "$tries" -lt 5 ]] || die "密码验证失败次数过多"
  done
}

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

log "解析 Release（cdn=${INSTALL_CDN}）"
resolve_version
assert_clean_tag "$RESOLVED_TAG"
pick_mirror_for_tag "$RESOLVED_TAG"
log "下载 ${ASSET} (${RESOLVED_TAG})"
download_asset "$RESOLVED_TAG" "$ASSET" "$WORKDIR/$ASSET"
if download_asset "$RESOLVED_TAG" "checksums.txt" "$WORKDIR/checksums.txt" 0; then
  verify_checksum "$WORKDIR" "$ASSET" "$WORKDIR/checksums.txt"
fi
chmod 755 "$WORKDIR/$ASSET"

systemctl stop 1pm-agent.service 2>/dev/null || true
install -m 755 "$WORKDIR/$ASSET" "$BIN_PATH"

log "写入配置 ${MASTER} (tls=${MASTER_TLS})"
INSTALL_ARGS=("$MASTER" "$TOKEN" --name "$NODE_NAME" --group "$NODE_GROUP")
if [[ "$MASTER_TLS" == "1" ]]; then
  INSTALL_ARGS+=(--master-tls)
fi
"$BIN_PATH" agent install "${INSTALL_ARGS[@]}" >/dev/null

save_panel_password

cat > "$UNIT_PATH" <<EOF
[Unit]
Description=1Panel multi-node agent tunnel
After=network-online.target 1panel-core.service
Wants=network-online.target

[Service]
Type=simple
Environment=HOME=/root
Environment=GITHUB_API=${GITHUB_API}
Environment=GITHUB_DL=${GITHUB_DL}
Environment=INSTALL_CDN=${INSTALL_CDN}
Environment=REPO=${REPO}
ExecStart=${BIN_PATH} agent run
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable 1pm-agent.service >/dev/null
systemctl restart 1pm-agent.service
sleep 1

echo
echo "安装完成  agent → ${MASTER}  (1pm ${RESOLVED_TAG} from CDN)"
if systemctl is-active --quiet 1pm-agent.service; then
  echo "  服务: active"
else
  echo "  服务: failed — journalctl -u 1pm-agent -e" >&2
  exit 1
fi
echo
`) + "\n"))
