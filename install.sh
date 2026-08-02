#!/usr/bin/env bash
# 1pm Master one-click installer
# Usage:
#   curl -fsSL <script-url> | sudo bash
#   curl -fsSL <script-url> | sudo PANEL_PASS='xxx' bash
#   curl -fsSL <script-url> | sudo INSTALL_CDN=cn VERSION=v0.0.1 bash
#
# INSTALL_CDN:  auto (default) | global | cn
# VERSION:      empty = latest GitHub release
# PANEL_PASS:   optional; enables node-switch auto-login
# REPO:         AnkioTomas/1panel-agent
set -euo pipefail

REPO="${REPO:-AnkioTomas/1panel-agent}"
INSTALL_CDN="${INSTALL_CDN:-auto}"
VERSION="${VERSION:-}"
PANEL_PASS="${PANEL_PASS:-}"
BIN_PATH="${BIN_PATH:-/usr/local/bin/1pm}"
UNIT_PATH="${UNIT_PATH:-/etc/systemd/system/1pm-master.service}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
GITHUB_DL="${GITHUB_DL:-https://github.com}"

# Domestic mirrors that prefix the original GitHub/API URL.
CN_MIRRORS=(
  "https://ghfast.top/"
  "https://gh-proxy.com/"
  "https://mirror.ghproxy.com/"
  "https://ghproxy.net/"
)

log()  { echo ">> $*"; }
warn() { echo "warn: $*" >&2; }
die()  { echo "error: $*" >&2; exit 1; }

need_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run as root (curl ... | sudo bash)"
}

# Master takeover requires a local 1Panel.
require_1panel() {
  local ok=0
  if [[ -f /opt/1panel/db/core.db ]]; then
    ok=1
  elif have_cmd 1pctl; then
    ok=1
  elif systemctl list-unit-files 1panel-core.service 2>/dev/null | grep -q 1panel-core.service; then
    ok=1
  elif [[ -x /usr/bin/1panel ]] || [[ -x /usr/local/bin/1panel ]]; then
    ok=1
  fi
  if [[ "$ok" -ne 1 ]]; then
    echo "error: 未检测到本机 1Panel，无法安装 Master。" >&2
    echo >&2
    echo "请先安装并启动 1Panel，再重新执行本脚本。" >&2
    echo "  官方安装: https://1panel.cn / https://github.com/1Panel-dev/1Panel" >&2
    echo >&2
    echo "检测项（任一存在即可）：" >&2
    echo "  - /opt/1panel/db/core.db" >&2
    echo "  - 命令 1pctl" >&2
    echo "  - systemd 单元 1panel-core.service" >&2
    exit 1
  fi
  if systemctl list-unit-files 1panel-core.service 2>/dev/null | grep -q 1panel-core.service; then
    if ! systemctl is-active --quiet 1panel-core.service; then
      warn "检测到 1panel-core.service 但未运行，尝试启动…"
      systemctl start 1panel-core.service || die "无法启动 1panel-core，请先修好 1Panel 再装 Master"
      sleep 1
    fi
  fi
  if [[ ! -f /opt/1panel/db/core.db ]]; then
    die "已检测到 1Panel 组件，但缺少 /opt/1panel/db/core.db（面板未初始化？）"
  fi
  log "1Panel OK: /opt/1panel/db/core.db"
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "unsupported arch: $(uname -m) (need amd64/arm64)" ;;
  esac
}

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  [[ "$os" == "linux" ]] || die "master install supports linux only (got $os)"
  echo "$os"
}

have_cmd() { command -v "$1" >/dev/null 2>&1; }

http_get() {
  # http_get URL OUT_FILE
  local url="$1" out="$2"
  if have_cmd curl; then
    curl -fsSL --connect-timeout 8 --max-time 120 -o "$out" "$url"
  elif have_cmd wget; then
    wget -q -O "$out" "$url"
  else
    die "need curl or wget"
  fi
}

http_ok() {
  # probe URL — return 0 if reachable
  local url="$1"
  if have_cmd curl; then
    curl -fsI --connect-timeout 5 --max-time 15 "$url" >/dev/null 2>&1
  elif have_cmd wget; then
    wget -q --spider --timeout=15 "$url" >/dev/null 2>&1
  else
    return 1
  fi
}

# Build candidate download bases: each is a prefix; append path without leading slash.
# global: https://github.com/ + OWNER/REPO/releases/download/TAG/FILE
# cn:     https://ghfast.top/https://github.com/ + ...
candidate_bases() {
  local mode="$1"
  case "$mode" in
    global)
      echo "${GITHUB_DL}/"
      ;;
    cn)
      local m
      for m in "${CN_MIRRORS[@]}"; do
        echo "${m}${GITHUB_DL}/"
      done
      ;;
    auto|*)
      echo "${GITHUB_DL}/"
      local m
      for m in "${CN_MIRRORS[@]}"; do
        echo "${m}${GITHUB_DL}/"
      done
      ;;
  esac
}

candidate_apis() {
  local mode="$1"
  case "$mode" in
    global)
      echo "${GITHUB_API}/"
      ;;
    cn)
      local m
      for m in "${CN_MIRRORS[@]}"; do
        echo "${m}${GITHUB_API}/"
      done
      # some mirrors only proxy github.com, not api — still try raw api last
      echo "${GITHUB_API}/"
      ;;
    auto|*)
      echo "${GITHUB_API}/"
      local m
      for m in "${CN_MIRRORS[@]}"; do
        echo "${m}${GITHUB_API}/"
      done
      ;;
  esac
}

resolve_version() {
  if [[ -n "$VERSION" ]]; then
    echo "$VERSION"
    return
  fi
  local api base tmp tag
  tmp="$(mktemp)"
  for base in $(candidate_apis "$INSTALL_CDN"); do
    api="${base}repos/${REPO}/releases/latest"
    if http_get "$api" "$tmp" 2>/dev/null; then
      tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp" | head -n1)"
      rm -f "$tmp"
      if [[ -n "$tag" ]]; then
        log "latest release: $tag (via ${base%%/})"
        echo "$tag"
        return
      fi
    fi
  done
  rm -f "$tmp"
  die "cannot resolve latest release (try VERSION=vX.Y.Z or INSTALL_CDN=cn)"
}

pick_base_for_asset() {
  # pick_base_for_asset TAG FILE -> prints working base prefix
  local tag="$1" file="$2" base url
  for base in $(candidate_bases "$INSTALL_CDN"); do
    url="${base}${REPO}/releases/download/${tag}/${file}"
    log "probe $url"
    if http_ok "$url"; then
      echo "$base"
      return
    fi
  done
  # last resort: try download anyway with first base (some mirrors break HEAD)
  base="$(candidate_bases "$INSTALL_CDN" | head -n1)"
  warn "HEAD probe failed; falling back to $base"
  echo "$base"
}

download_asset() {
  # download_asset BASE TAG FILE OUT [required=1]
  local base="$1" tag="$2" file="$3" out="$4" required="${5:-1}" url b
  url="${base}${REPO}/releases/download/${tag}/${file}"
  log "download $file"
  if http_get "$url" "$out" 2>/dev/null; then
    return 0
  fi
  for b in $(candidate_bases "$INSTALL_CDN"); do
    [[ "$b" == "$base" ]] && continue
    url="${b}${REPO}/releases/download/${tag}/${file}"
    log "retry $url"
    if http_get "$url" "$out" 2>/dev/null; then
      return 0
    fi
  done
  if [[ "$required" == "1" ]]; then
    die "download failed: $file"
  fi
  warn "optional download failed: $file"
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
  elif have_cmd shasum; then
    local want got
    want="$(awk -v f="$file" '$2==f {print $1; exit}' "$sumfile")"
    [[ -n "$want" ]] || { warn "checksum entry missing for $file"; return 0; }
    got="$(shasum -a 256 "$dir/$file" | awk '{print $1}')"
    [[ "$want" == "$got" ]] || die "checksum mismatch for $file"
  else
    warn "no sha256sum/shasum; skip verify"
  fi
}

write_unit() {
  cat > "$UNIT_PATH" <<'EOF'
[Unit]
Description=1Panel multi-node master gateway
After=network-online.target 1panel-core.service
Wants=network-online.target

[Service]
Type=simple
# State in /var/lib/1pm/master.json — do not put secrets on ExecStart.
ExecStart=/usr/local/bin/1pm master
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
}

maybe_set_password() {
  if [[ -n "$PANEL_PASS" ]]; then
    log "save panel password for node-switch login"
    "$BIN_PATH" master set --panel-pass "$PANEL_PASS"
    return
  fi
  if [[ -t 0 ]]; then
    local pass
    read -r -s -p "Panel password for node-switch login (Enter to skip): " pass
    echo
    if [[ -n "$pass" ]]; then
      "$BIN_PATH" master set --panel-pass "$pass"
    else
      warn "skipped — later: 1pm master set --panel-pass PASS"
    fi
  else
    warn "no PANEL_PASS and no TTY — later: 1pm master set --panel-pass PASS"
  fi
}

# Read key=value from master.json without requiring jq.
json_str() {
  local key="$1" file="$2"
  [[ -f "$file" ]] || return 0
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$file" | head -n1
}

json_num() {
  local key="$1" file="$2"
  [[ -f "$file" ]] || return 0
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p" "$file" | head -n1
}

detect_lan_ip() {
  local ip
  ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')"
  if [[ -z "$ip" ]]; then
    ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  fi
  echo "${ip:-<本机IP>}"
}

print_next_steps() {
  local ver="$1" state="/var/lib/1pm/master.json"
  local host port entrance ui agent_hint
  # give master a moment to write master.json after takeover
  local i
  for i in 1 2 3 4 5; do
    [[ -f "$state" ]] && break
    sleep 1
  done

  host="$(json_str public_host "$state")"
  [[ -z "$host" ]] && host="$(detect_lan_ip)"
  port="$(json_num original_port "$state")"
  [[ -z "$port" ]] && port="<面板端口>"
  entrance="$(json_str entrance "$state")"

  if [[ -n "$entrance" ]]; then
    ui="http://${host}:${port}/${entrance}"
  else
    ui="http://${host}:${port}/"
  fi
  agent_hint="http://${host}:${port}/__mp/"

  echo
  echo "========================================"
  echo " 1pm Master 安装完成 (${ver})"
  echo "========================================"
  echo
  echo "接下来请按顺序操作："
  echo
  echo "  1) 浏览器打开本机 1Panel 并登录"
  echo "     ${ui}"
  echo
  echo "  2) 登录后打开多机管理页"
  echo "     ${agent_hint}"
  echo "     （侧边栏「多机节点」也可进入）"
  echo
  echo "  3) 在管理页复制「子节点安装命令」"
  echo "     到每台 Agent 机器以 root 执行（curl | bash）"
  echo
  echo "  4) 子节点上线后，点「进入面板」切换；"
  echo "     子节点 1Panel 自己处理登录"
  echo
  echo "常用命令："
  echo "  systemctl status 1pm-master"
  echo "  journalctl -u 1pm-master -f"
  echo "  # 升级/重装：重新执行本安装脚本即可"
  echo
  if ! systemctl is-active --quiet 1pm-master.service; then
    warn "服务未处于 active，请检查: journalctl -u 1pm-master -e"
  fi
}

main() {
  need_root
  have_cmd systemctl || die "systemd required"
  require_1panel

  local os arch tag asset base tmpdir ver
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="1pm_${os}_${arch}"

  log "cdn=${INSTALL_CDN} repo=${REPO}"
  tag="$(resolve_version)"
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  base="$(pick_base_for_asset "$tag" "$asset")"
  download_asset "$base" "$tag" "$asset" "$tmpdir/$asset" 1
  download_asset "$base" "$tag" "checksums.txt" "$tmpdir/checksums.txt" 0 || true
  verify_checksum "$tmpdir" "$asset" "$tmpdir/checksums.txt"

  systemctl stop 1pm-master.service 2>/dev/null || true
  install -m 755 "$tmpdir/$asset" "$BIN_PATH"
  write_unit
  maybe_set_password

  systemctl daemon-reload
  systemctl enable 1pm-master.service
  systemctl restart 1pm-master.service
  sleep 1

  ver="$("$BIN_PATH" version 2>/dev/null || echo "$tag")"
  log "installed ${ver} -> $BIN_PATH"
  print_next_steps "$ver"
}

main "$@"
