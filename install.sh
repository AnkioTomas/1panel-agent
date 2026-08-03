#!/usr/bin/env bash
# 1pm Master one-click installer
#
# 拉脚本（不要用 @main — jsDelivr 会永久缓存旧文件）：
#   curl -fsSL https://cdn.jsdelivr.net/gh/AnkioTomas/1panel-agent@v0.0.3/install.sh | sudo bash
#   curl -fsSL https://cdn.akams.cn/jsd/gh/AnkioTomas/1panel-agent@v0.0.3/install.sh | sudo bash
#   curl -fsSL https://gh-proxy.com/https://github.com/AnkioTomas/1panel-agent/releases/latest/download/install.sh | sudo bash
#
# INSTALL_CDN: auto (default) | global | cn
# VERSION:     empty = latest GitHub release
set -euo pipefail

REPO="${REPO:-AnkioTomas/1panel-agent}"
INSTALL_CDN="${INSTALL_CDN:-auto}"
VERSION="${VERSION:-}"
BIN_PATH="${BIN_PATH:-/usr/local/bin/1pm}"
UNIT_PATH="${UNIT_PATH:-/etc/systemd/system/1pm-master.service}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
GITHUB_DL="${GITHUB_DL:-https://github.com}"

# 少量相对靠谱的 GitHub 前缀代理（https://host/ + https://github.com/...）
# 不搞 70+ 节点测速马戏：API 通不代表 Release 大文件通，逐个真实 GET 更靠谱。
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

# 日志必须进 stderr，否则 tag="$(...)" 会把日志吃进版本号。
log()  { echo "==> $*" >&2; }
warn() { echo "warn: $*" >&2; }
die()  { echo "error: $*" >&2; exit 1; }

PREFERRED_MIRROR=""
RESOLVED_TAG=""

strip_ansi() {
  sed $'s/\x1b\\[[0-9;]*[a-zA-Z]//g' | tr -d '\r'
}

need_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run as root (curl ... | sudo bash)"
}

have_cmd() { command -v "$1" >/dev/null 2>&1; }

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

# 返回候选前缀列表：空字符串 = 直连；其余为 https://proxy/
mirror_prefixes() {
  local mode="$1" m
  case "$mode" in
    global)
      printf '%s\n' ""
      ;;
    cn)
      for m in "${MIRROR_PREFIXES[@]}"; do
        printf '%s\n' "$m"
      done
      printf '%s\n' ""
      ;;
    auto|*)
      if [[ -n "$PREFERRED_MIRROR" ]]; then
        printf '%s\n' "$PREFERRED_MIRROR"
      fi
      for m in "${MIRROR_PREFIXES[@]}"; do
        printf '%s\n' "$m"
      done
      printf '%s\n' ""
      ;;
  esac
}

# http_get URL OUT — 打印失败状态到 stderr
http_get() {
  local url="$1" out="$2" code=0
  if have_cmd curl; then
    code="$(curl -L --connect-timeout 15 --max-time 180 --retry 1 --retry-delay 1 \
      -o "$out" -w '%{http_code}' "$url" 2>/dev/null || echo 000)"
    if [[ "$code" =~ ^2[0-9][0-9]$ ]] && [[ -s "$out" ]]; then
      return 0
    fi
    warn "HTTP ${code}: $url"
    rm -f "$out"
    return 1
  elif have_cmd wget; then
    if wget -q -O "$out" --timeout=180 "$url" && [[ -s "$out" ]]; then
      return 0
    fi
    warn "wget fail: $url"
    rm -f "$out"
    return 1
  fi
  die "need curl or wget"
}

# 用真实 Release 小文件探测哪个代理能下（API 通 ≠ 大文件通）
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
  local api prefix url tmp tag

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
        log "latest release: $tag (via ${url%%/repos/*})"
        RESOLVED_TAG="$tag"
        return
      fi
    fi
  done < <(mirror_prefixes "$INSTALL_CDN")
  rm -f "$tmp"
  die "cannot resolve latest release (try VERSION=vX.Y.Z)"
}

assert_clean_tag() {
  local tag="$1"
  if [[ "$tag" == *$'\n'* || "$tag" == *'==>'* || "$tag" == *' '* ]]; then
    die "脚本版本号被污染（多半是旧 install.sh / jsDelivr @main 缓存）。请改用 Release 或带版本 tag 的 URL，见脚本头部注释。"
  fi
  [[ "$tag" =~ ^v[0-9] ]] || die "invalid release tag: $tag"
}

download_asset() {
  # download_asset TAG FILE OUT [required=1]
  local tag="$1" file="$2" out="$3" required="${4:-1}"
  local prefix url tried="" ok=0
  local order=()

  # 已探测到的节点优先（空字符串 = 直连）
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

  if [[ -f /etc/systemd/system/1pm-agent.service ]] || [[ -f /root/.1panel-agent/agent.json ]]; then
    die "本机已安装 1pm agent，不能同时作为 master。先执行: 1pm uninstall"
  fi

  local os arch tag asset tmpdir ver
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="1pm_${os}_${arch}"

  resolve_version
  tag="$RESOLVED_TAG"
  assert_clean_tag "$tag"
  log "安装 ${tag} (${os}/${arch})"

  pick_mirror_for_tag "$tag"

  tmpdir="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap 'rm -rf "'"$tmpdir"'"' EXIT

  log "下载二进制…"
  download_asset "$tag" "$asset" "$tmpdir/$asset" 1
  # checksum 可能已在探测时下过；再拉一次无妨
  download_asset "$tag" "checksums.txt" "$tmpdir/checksums.txt" 0 || true
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
