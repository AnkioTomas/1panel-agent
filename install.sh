#!/usr/bin/env bash
# 1pm Master one-click installer
# Usage:
#   # 推荐：仓库内 install.sh，经 jsDelivr / akams 加速
#   curl -fsSL https://cdn.jsdelivr.net/gh/AnkioTomas/1panel-agent@main/install.sh | sudo bash
#   curl -fsSL https://cdn.akams.cn/jsd/gh/AnkioTomas/1panel-agent@main/install.sh | sudo bash
#
#   # 或 GitHub Release 附件
#   curl -fsSL https://github.com/AnkioTomas/1panel-agent/releases/latest/download/install.sh | sudo bash
#
# INSTALL_CDN:  auto (default) | global | cn
# VERSION:      empty = latest GitHub release
# REPO:         AnkioTomas/1panel-agent
#
# 二进制下载：auto/cn 会从 https://github.akams.cn/ 节点列表测速，
# 选延迟最低的代理，按 https://<node>/https://github.com/... 拉取。
set -euo pipefail

REPO="${REPO:-AnkioTomas/1panel-agent}"
INSTALL_CDN="${INSTALL_CDN:-auto}"
VERSION="${VERSION:-}"
BIN_PATH="${BIN_PATH:-/usr/local/bin/1pm}"
UNIT_PATH="${UNIT_PATH:-/etc/systemd/system/1pm-master.service}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
GITHUB_DL="${GITHUB_DL:-https://github.com}"
AKAMS_HOME="${AKAMS_HOME:-https://github.akams.cn}"

# akams 贡献节点种子（页面动态列表拉取失败时使用；格式: host，不含 https://）
AKAMS_SEED_NODES=(
  gh.dpik.top
  github.tbap.top
  ghfile.geekertao.top
  ghproxy.net
  gh-proxy.com
  gh-proxy.net
  cdn.gh-proxy.com
  github.dpik.top
  github-proxy.memory-echoes.cn
  gh.felicity.ac.cn
  ghfast.top
  gh.monlor.com
  gh.ddlc.top
  gitproxy.click
  ghpr.cc
  gh.sixyin.com
  gh.jasonzeng.dev
  gh.idayer.com
  ghproxy.felicity.land
  gh.tryxd.cn
  gitproxy.mrhjx.cn
  gh.chjina.com
  gh.noki.icu
  gh.acmsz.top
  gh.catmak.name
)

# 测速后的代理前缀：https://host/  （后面拼 https://github.com/...）
RANKED_MIRRORS=()

# 日志必须进 stderr：否则 command substitution 会把日志吃进变量。
log()  { echo "==> $*" >&2; }
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
  # http_get URL OUT_FILE — 成功返回 0；失败保留非零，不吞错误码
  local url="$1" out="$2"
  if have_cmd curl; then
    curl -fL --connect-timeout 12 --max-time 180 --retry 1 --retry-delay 1 -o "$out" "$url"
  elif have_cmd wget; then
    wget -q -O "$out" --timeout=180 "$url"
  else
    die "need curl or wget"
  fi
}

# API 成功时记录的镜像前缀（如 https://gh.dpik.top/），下载优先用同一镜像。
# 注意：不能用 tag="$(resolve_version)" —— 命令替换是子 shell，全局变量会丢。
PREFERRED_MIRROR=""
RESOLVED_TAG=""

# 合法代理主机名（拒绝中文域名等，避免 URL 编码坑）
_is_proxy_host() {
  [[ "$1" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?\.[A-Za-z]{2,}$ ]]
}

# 从 github.akams.cn 前端包解析节点列表（失败则静默返回）
fetch_akams_nodes() {
  have_cmd curl || return 1
  local html jsdir path f out
  html="$(curl -fsSL --connect-timeout 8 --max-time 15 "$AKAMS_HOME/" 2>/dev/null)" || return 1
  jsdir="$(mktemp -d)"
  out="$(mktemp)"
  while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    curl -fsSL --connect-timeout 5 --max-time 12 "${AKAMS_HOME}${path}" \
      -o "$jsdir/${path##*/}" 2>/dev/null || true
  done < <(printf '%s' "$html" | grep -oE '/_next/static/chunks/[^" ]+\.js' | sort -u)
  for f in "$jsdir"/*.js; do
    [[ -f "$f" ]] || continue
    grep -oE 'value:"[A-Za-z0-9][A-Za-z0-9.-]+\.[A-Za-z]{2,}"' "$f" 2>/dev/null \
      | sed 's/value:"//;s/"$//' >> "$out" || true
  done
  if [[ -s "$out" ]]; then
    sort -u "$out"
    rm -rf "$jsdir" "$out"
    return 0
  fi
  rm -rf "$jsdir" "$out"
  return 1
}

# 合并种子 + 在线列表 → 去重主机名
collect_proxy_hosts() {
  local h seen=$'\n'
  for h in "${AKAMS_SEED_NODES[@]}"; do
    _is_proxy_host "$h" || continue
    case "$seen" in
      *$'\n'"$h"$'\n'*) continue ;;
    esac
    seen="${seen}${h}"$'\n'
    printf '%s\n' "$h"
  done
  while IFS= read -r h; do
    _is_proxy_host "$h" || continue
    case "$seen" in
      *$'\n'"$h"$'\n'*) continue ;;
    esac
    seen="${seen}${h}"$'\n'
    printf '%s\n' "$h"
  done < <(fetch_akams_nodes 2>/dev/null || true)
}

# 并行测速：对 https://host/https://api.github.com/... 发短请求，按耗时排序写入 RANKED_MIRRORS
rank_mirrors() {
  RANKED_MIRRORS=()
  [[ "$INSTALL_CDN" == "global" ]] && return 0

  local probe="${GITHUB_API}/repos/${REPO}/releases/latest"
  local hosts=() h dir code t line n=0 max_probe=24
  local -a pids=()

  log "测速加速节点（来源: ${AKAMS_HOME}）…"
  while IFS= read -r h; do
    hosts+=("$h")
  done < <(collect_proxy_hosts)

  [[ ${#hosts[@]} -eq 0 ]] && return 0

  dir="$(mktemp -d)"
  for h in "${hosts[@]}"; do
    [[ $n -ge $max_probe ]] && break
    n=$((n + 1))
    (
      # http_code time_total host
      if have_cmd curl; then
        line="$(curl -sS -o /dev/null -w '%{http_code} %{time_total}' \
          --connect-timeout 3 --max-time 6 -L "https://${h}/${probe}" 2>/dev/null || true)"
      else
        line=""
      fi
      code="${line%% *}"
      t="${line##* }"
      [[ "$code" =~ ^2[0-9][0-9]$ ]] || exit 0
      printf '%s %s\n' "$t" "$h" > "$dir/$h"
    ) &
    pids+=($!)
    # 限制并发，避免把脚本/对方都打挂
    if (( ${#pids[@]} >= 8 )); then
      wait "${pids[0]}" 2>/dev/null || true
      pids=("${pids[@]:1}")
    fi
  done
  for pid in "${pids[@]+"${pids[@]}"}"; do
    wait "$pid" 2>/dev/null || true
  done

  local ranked=()
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    h="${line##* }"
    ranked+=("https://${h}/")
    log "  可用 ${h} (${line%% *}s)"
  done < <(cat "$dir"/* 2>/dev/null | sort -n | head -n 8)
  rm -rf "$dir"

  RANKED_MIRRORS=("${ranked[@]}")
  if [[ ${#RANKED_MIRRORS[@]} -eq 0 ]]; then
    warn "节点测速无可用结果，回退种子列表顺序尝试"
    for h in "${AKAMS_SEED_NODES[@]}"; do
      _is_proxy_host "$h" || continue
      RANKED_MIRRORS+=("https://${h}/")
    done
  else
    log "选用最快节点: ${RANKED_MIRRORS[0]#https://}"
  fi
}

_emit_unique() {
  local value="$1"
  case "$__SEEN_BASES" in
    *"|${value}|"*) return ;;
  esac
  __SEEN_BASES="${__SEEN_BASES}|${value}|"
  printf '%s\n' "$value"
}

# 下载 base：前缀 + github.com/ + REPO/releases/download/...
candidate_bases() {
  local mode="$1" m
  __SEEN_BASES=""
  if [[ -n "$PREFERRED_MIRROR" ]]; then
    _emit_unique "${PREFERRED_MIRROR}${GITHUB_DL}/"
  fi
  case "$mode" in
    global)
      _emit_unique "${GITHUB_DL}/"
      ;;
    cn|auto|*)
      for m in "${RANKED_MIRRORS[@]+"${RANKED_MIRRORS[@]}"}"; do
        _emit_unique "${m}${GITHUB_DL}/"
      done
      _emit_unique "${GITHUB_DL}/"
      ;;
  esac
}

candidate_apis() {
  local mode="$1" m
  case "$mode" in
    global)
      printf '%s\n' "${GITHUB_API}/"
      ;;
    cn|auto|*)
      for m in "${RANKED_MIRRORS[@]+"${RANKED_MIRRORS[@]}"}"; do
        printf '%s\n' "${m}${GITHUB_API}/"
      done
      printf '%s\n' "${GITHUB_API}/"
      ;;
  esac
}

# 从 api base（…/api.github.com/ 或 mirror+api）提取镜像前缀；直连则空。
mirror_prefix_from_api_base() {
  local base="$1"
  case "$base" in
    "${GITHUB_API}/") echo "" ;;
    *"https://api.github.com/"*) echo "${base%https://api.github.com/}" ;;
    *) echo "" ;;
  esac
}

resolve_version() {
  # 结果写入 RESOLVED_TAG / PREFERRED_MIRROR（勿用 stdout 捕获）
  RESOLVED_TAG=""
  if [[ -n "$VERSION" ]]; then
    local api base tmp
    tmp="$(mktemp)"
    while IFS= read -r base; do
      [[ -n "$base" ]] || continue
      api="${base}repos/${REPO}/releases/tags/${VERSION}"
      if http_get "$api" "$tmp" >/dev/null 2>&1; then
        PREFERRED_MIRROR="$(mirror_prefix_from_api_base "$base")"
        rm -f "$tmp"
        RESOLVED_TAG="$VERSION"
        return
      fi
    done < <(candidate_apis "$INSTALL_CDN")
    rm -f "$tmp"
    RESOLVED_TAG="$VERSION"
    return
  fi
  local api base tmp tag
  tmp="$(mktemp)"
  while IFS= read -r base; do
    [[ -n "$base" ]] || continue
    api="${base}repos/${REPO}/releases/latest"
    if http_get "$api" "$tmp" >/dev/null 2>&1; then
      tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp" | head -n1)"
      if [[ -n "$tag" ]]; then
        PREFERRED_MIRROR="$(mirror_prefix_from_api_base "$base")"
        rm -f "$tmp"
        log "latest release: $tag (via ${base%/})"
        RESOLVED_TAG="$tag"
        return
      fi
    fi
  done < <(candidate_apis "$INSTALL_CDN")
  rm -f "$tmp"
  die "cannot resolve latest release (try VERSION=vX.Y.Z or INSTALL_CDN=cn)"
}

download_asset() {
  # download_asset TAG FILE OUT [required=1]
  local tag="$1" file="$2" out="$3" required="${4:-1}"
  local base url tried="" ok=0
  while IFS= read -r base; do
    [[ -n "$base" ]] || continue
    url="${base}${REPO}/releases/download/${tag}/${file}"
    tried="${tried}${tried:+ }${url}"
    if http_get "$url" "$out" >/dev/null 2>&1 && [[ -s "$out" ]]; then
      log "已下载 $file <- ${base%/}"
      ok=1
      break
    fi
    warn "下载失败: $url"
    rm -f "$out"
  done < <(candidate_bases "$INSTALL_CDN")
  if [[ "$ok" -eq 1 ]]; then
    return 0
  fi
  if [[ "$required" == "1" ]]; then
    die "download failed: $file（已尝试: $tried）"
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

  # 一台机器不能同时是 agent 和 master
  if [[ -f /etc/systemd/system/1pm-agent.service ]] || [[ -f /root/.1panel-agent/agent.json ]]; then
    die "本机已安装 1pm agent，不能同时作为 master。先执行: 1pm uninstall"
  fi

  local os arch tag asset tmpdir ver
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="1pm_${os}_${arch}"

  rank_mirrors
  resolve_version
  tag="$RESOLVED_TAG"
  [[ "$tag" =~ ^v?[0-9] ]] || die "invalid release tag: $tag"
  log "安装 ${tag} (${os}/${arch})"
  tmpdir="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap 'rm -rf "'"$tmpdir"'"' EXIT

  log "下载二进制…"
  download_asset "$tag" "$asset" "$tmpdir/$asset" 1
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
