#!/usr/bin/env bash
# 1pm 手动更新脚本 — 用于 1pm update 失败时强制替换二进制
# 用法: curl -fsSL https://gh-proxy.com/https://raw.githubusercontent.com/AnkioTomas/1panel-agent/v0.1.7/deploy/force-update.sh | sudo bash
#       或: VERSION=v0.1.7 curl -fsSL <url> | sudo bash
set -euo pipefail

REPO="${REPO:-AnkioTomas/1panel-agent}"
INSTALL_CDN="${INSTALL_CDN:-auto}"
VERSION="${VERSION:-}"
BIN_PATH="${BIN_PATH:-/usr/local/bin/1pm}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
GITHUB_DL="${GITHUB_DL:-https://github.com}"

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

log()  { echo "==> $*" >&2; }
warn() { echo "warn: $*" >&2; }
die()  { echo "error: $*" >&2; exit 1; }

PREFERRED_MIRROR=""

need_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run as root"
}

have_cmd() { command -v "$1" >/dev/null 2>&1; }

# 返回候选前缀列表
mirror_prefixes() {
  local mode="$1" m
  case "$mode" in
    global)
      printf '%s\n' ""
      ;;
    cn)
      for m in "${MIRROR_PREFIXES[@]}"; do printf '%s\n' "$m"; done
      printf '%s\n' ""
      ;;
    auto|*)
      if [[ -n "$PREFERRED_MIRROR" ]]; then
        printf '%s\n' "$PREFERRED_MIRROR"
      fi
      for m in "${MIRROR_PREFIXES[@]}"; do printf '%s\n' "$m"; done
      printf '%s\n' ""
      ;;
  esac
}

http_get() {
  local url="$1" out="$2" code=0
  if have_cmd curl; then
    code="$(curl -L --connect-timeout 15 --max-time 600 --retry 2 --retry-delay 2 \
      -o "$out" -w '%{http_code}' "$url" 2>/dev/null || echo 000)"
    if [[ "$code" =~ ^2[0-9][0-9]$ ]] && [[ -s "$out" ]]; then
      return 0
    fi
    warn "HTTP ${code}: $url"
    rm -f "$out"
    return 1
  elif have_cmd wget; then
    if wget -q -O "$out" --timeout=600 "$url" && [[ -s "$out" ]]; then
      return 0
    fi
    warn "wget fail: $url"
    rm -f "$out"
    return 1
  fi
  die "need curl or wget"
}

download_asset() {
  local tag="$1" file="$2" out="$3"
  local prefix url tried="" ok=0
  local order=()
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
    log "  尝试 ${prefix:-github.com(直连)} ..."
    if http_get "$url" "$out"; then
      log "  下载成功!"
      ok=1
      break
    fi
  done

  if [[ "$ok" -eq 1 ]]; then
    return 0
  fi
  die "下载失败: $file"$'\n'"已尝试: $tried"
}

resolve_version() {
  if [[ -n "$VERSION" ]]; then
    log "使用指定版本: $VERSION"
    return
  fi

  log "探测最新版本..."
  local api prefix url tmp tag
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
        VERSION="$tag"
        log "最新版本: $tag (via ${url%%/repos/*})"
        return
      fi
    fi
  done < <(mirror_prefixes "$INSTALL_CDN")
  rm -f "$tmp"
  die "无法获取最新版本 (可设置 VERSION=vX.Y.Z 指定版本)"
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "unsupported arch: $(uname -m)" ;;
  esac
}

detect_mode() {
  # 判断本机是 master 还是 agent
  if [[ -f /etc/systemd/system/1pm-master.service ]] || [[ -f /var/lib/1pm/master.json ]]; then
    echo "master"
  elif [[ -f /etc/systemd/system/1pm-agent.service ]] || [[ -f /root/.1panel-agent/agent.json ]]; then
    echo "agent"
  else
    die "未检测到 1pm master 或 agent 模式"
  fi
}

main() {
  need_root
  have_cmd systemctl || die "systemd required"

  local arch mode old_ver
  arch="$(detect_arch)"
  mode="$(detect_mode)"
  old_ver="$($BIN_PATH version 2>/dev/null || echo "unknown")"

  log "当前版本: $old_ver"
  log "本机模式: $mode"
  log "架构: $arch"

  resolve_version

  local asset="1pm_linux_${arch}"
  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "'"$tmpdir"'"' EXIT

  pick_mirror_for_tag() {
    PREFERRED_MIRROR=""
    [[ "$INSTALL_CDN" == "global" ]] && return 0
    local tag="$1" prefix url tmp
    tmp="$(mktemp)"
    log "探测可用下载节点..."
    while IFS= read -r prefix; do
      if [[ -z "$prefix" ]]; then
        url="${GITHUB_DL}/${REPO}/releases/download/${tag}/checksums.txt"
      else
        url="${prefix}${GITHUB_DL}/${REPO}/releases/download/${tag}/checksums.txt"
      fi
      if http_get "$url" "$tmp"; then
        PREFERRED_MIRROR="$prefix"
        if [[ -z "$prefix" ]]; then
          log "下载节点: github.com(直连)"
        else
          log "下载节点: ${prefix#https://}"
        fi
        rm -f "$tmp"
        return 0
      fi
    done < <(mirror_prefixes "$INSTALL_CDN")
    rm -f "$tmp"
    warn "未找到可用下载节点，将逐个重试"
  }

  pick_mirror_for_tag "$VERSION"

  log "下载 ${VERSION} (${asset})..."
  download_asset "$VERSION" "$asset" "$tmpdir/$asset"

  # 可选 checksum
  if download_asset "$VERSION" "checksums.txt" "$tmpdir/checksums.txt" 0; then
    if have_cmd sha256sum; then
      local line
      line="$(grep -E "[[:space:]]${asset}$" "$tmpdir/checksums.txt" || true)"
      if [[ -n "$line" ]]; then
        (cd "$tmpdir" && echo "$line" | sha256sum -c -) || warn "checksum 校验失败，跳过"
      else
        warn "checksums.txt 中未找到 $asset"
      fi
    fi
  fi

  # 替换二进制
  log "替换二进制..."
  if [[ "$mode" == "master" ]]; then
    systemctl stop 1pm-master.service 2>/dev/null || true
  else
    systemctl stop 1pm-agent.service 2>/dev/null || true
  fi

  cp "$BIN_PATH" "${BIN_PATH}.bak.$(date +%s)"
  install -m 755 "$tmpdir/$asset" "$BIN_PATH"

  if [[ "$mode" == "master" ]]; then
    systemctl restart 1pm-master.service
  else
    systemctl restart 1pm-agent.service
  fi

  sleep 1

  local new_ver
  new_ver="$($BIN_PATH version 2>/dev/null || echo "$VERSION")"
  log ""
  log "更新完成!"
  log "  旧版本: $old_ver"
  log "  新版本: $new_ver"
  log "  备份: ${BIN_PATH}.bak.$(date +%s)"
  log ""
  log "查看服务状态: systemctl status 1pm-${mode}.service"
}

main "$@"
