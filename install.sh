#!/usr/bin/env bash
# 1pm Master one-click installer
# Usage:
#   curl -fsSL <script-url> | sudo bash
#   curl -fsSL <script-url> | sudo INSTALL_CDN=cn VERSION=v0.0.1 bash
#
# INSTALL_CDN:  auto (default) | global | cn
# VERSION:      empty = latest GitHub release
# REPO:         AnkioTomas/1panel-agent
set -euo pipefail

REPO="${REPO:-AnkioTomas/1panel-agent}"
INSTALL_CDN="${INSTALL_CDN:-auto}"
VERSION="${VERSION:-}"
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

log()  { echo "==> $*"; }
warn() { echo "warn: $*" >&2; }
die()  { echo "error: $*" >&2; exit 1; }

strip_ansi() {
  # drop CSI sequences from 1panel/1pctl colored output
  sed $'s/\x1b\\[[0-9;]*[a-zA-Z]//g' | tr -d '\r'
}

need_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run as root (curl ... | sudo bash)"
}

# Master takeover requires a local 1Panel (detect via CLI, never poke core.db).
require_1panel() {
  if ! have_cmd 1pctl && ! have_cmd 1panel; then
    die "未检测到 1Panel（需要 1pctl/1panel）。请先安装: https://1panel.cn"
  fi
  if systemctl list-unit-files 1panel-core.service 2>/dev/null | grep -q 1panel-core.service; then
    if ! systemctl is-active --quiet 1panel-core.service; then
      log "启动 1panel-core…"
      systemctl start 1panel-core.service || die "无法启动 1panel-core"
      sleep 1
    fi
  fi
  local ver=""
  if have_cmd 1pctl; then
    1pctl user-info >/dev/null 2>&1 || die "1pctl user-info 失败：面板未初始化或权限不足"
    ver="$(1pctl version 2>/dev/null | strip_ansi | sed -n 's/.*[Vv]ersion[[:space:]]*:[[:space:]]*//p;s/.*版本[[:space:]]*:[[:space:]]*//p' | head -1 | tr -d '[:space:]')"
  else
    1panel -l en user-info >/dev/null 2>&1 || die "1panel user-info 失败：面板未初始化或权限不足"
    ver="$(1panel -l en version 2>/dev/null | strip_ansi | sed -n 's/.*[Vv]ersion[[:space:]]*:[[:space:]]*//p;s/.*版本[[:space:]]*:[[:space:]]*//p' | head -1 | tr -d '[:space:]')"
  fi
  if [[ -n "$ver" ]]; then
    log "1Panel ${ver}"
  else
    log "1Panel OK"
  fi
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
    if http_ok "$url"; then
      echo "$base"
      return
    fi
  done
  base="$(candidate_bases "$INSTALL_CDN" | head -n1)"
  echo "$base"
}

download_asset() {
  # download_asset BASE TAG FILE OUT [required=1]
  local base="$1" tag="$2" file="$3" out="$4" required="${5:-1}" url b
  url="${base}${REPO}/releases/download/${tag}/${file}"
  if http_get "$url" "$out" 2>/dev/null; then
    return 0
  fi
  for b in $(candidate_bases "$INSTALL_CDN"); do
    [[ "$b" == "$base" ]] && continue
    url="${b}${REPO}/releases/download/${tag}/${file}"
    if http_get "$url" "$out" 2>/dev/null; then
      return 0
    fi
  done
  if [[ "$required" == "1" ]]; then
    die "download failed: $file"
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

# panel_entrance 从 1panel CLI 读安全入口路径段（master.json 不存该字段）。
panel_entrance() {
  local raw path
  if have_cmd 1pctl; then
    raw="$(1pctl user-info 2>/dev/null | strip_ansi)"
  else
    raw="$(1panel -l en user-info 2>/dev/null | strip_ansi)"
  fi
  path="$(printf '%s\n' "$raw" | sed -n 's|.*https\?://[^/]*/\([^[:space:]]*\).*|\1|p' | head -1)"
  path="${path%/}"
  echo "$path"
}

print_next_steps() {
  local ver="$1" state="/var/lib/1pm/master.json"
  local host port entrance ui mp
  local i
  for i in 1 2 3 4 5; do
    [[ -f "$state" ]] && break
    sleep 1
  done

  host="$(json_str public_host "$state")"
  [[ -z "$host" ]] && host="$(detect_lan_ip)"
  port="$(json_num original_port "$state")"
  [[ -z "$port" ]] && port="<面板端口>"
  entrance="$(panel_entrance)"

  if [[ -n "$entrance" ]]; then
    ui="http://${host}:${port}/${entrance}"
  else
    ui="http://${host}:${port}/"
  fi
  mp="http://${host}:${port}/__mp/"

  echo
  echo "安装完成  1pm ${ver}"
  echo
  echo "  1. 登录本机 1Panel"
  echo "     ${ui}"
  echo "  2. 打开多机管理"
  echo "     ${mp}"
  echo "  3. 复制「子节点安装命令」到 Agent 机器执行"
  echo
  if systemctl is-active --quiet 1pm-master.service; then
    echo "  服务: active"
  else
    warn "服务未 active，查看: journalctl -u 1pm-master -e"
  fi
  echo
}

main() {
  need_root
  have_cmd systemctl || die "systemd required"
  require_1panel

  local os arch tag asset base tmpdir ver
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="1pm_${os}_${arch}"

  tag="$(resolve_version)"
  log "安装 ${tag} (${os}/${arch})"
  tmpdir="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap 'rm -rf "'"$tmpdir"'"' EXIT

  base="$(pick_base_for_asset "$tag" "$asset")"
  log "下载二进制…"
  download_asset "$base" "$tag" "$asset" "$tmpdir/$asset" 1
  download_asset "$base" "$tag" "checksums.txt" "$tmpdir/checksums.txt" 0 || true
  verify_checksum "$tmpdir" "$asset" "$tmpdir/checksums.txt"

  systemctl stop 1pm-master.service 2>/dev/null || true
  install -m 755 "$tmpdir/$asset" "$BIN_PATH"
  write_unit

  systemctl daemon-reload
  systemctl enable 1pm-master.service >/dev/null
  systemctl restart 1pm-master.service
  sleep 1

  ver="$("$BIN_PATH" version 2>/dev/null || echo "$tag")"
  print_next_steps "$ver"
}

main "$@"
