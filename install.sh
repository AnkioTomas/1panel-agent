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

main() {
  need_root
  have_cmd systemctl || die "systemd required"

  local os arch tag asset base tmpdir
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

  log "installed $($BIN_PATH version 2>/dev/null || echo "$tag") -> $BIN_PATH"
  systemctl --no-pager --full status 1pm-master.service || true
  echo
  log "UI: http://<this-host>:<1panel-port>/__mp/  (login 1Panel first)"
  log "agent install command is shown on that page"
  log "re-run this script anytime to upgrade/reinstall"
}

main "$@"
