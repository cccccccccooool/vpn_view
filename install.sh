#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="vpnview"
SERVICE_NAME="vpnview"
ETC_DIR="/etc/vpnview"
DATA_DIR="${ETC_DIR}/data"
CONFIG_FILE="${ETC_DIR}/config.yaml"
TEMPLATE_FILE="${ETC_DIR}/singbox_template.json"
INSTALL_BIN="/usr/local/bin/vpnview"
DEFAULT_SINGBOX_CONFIG="/etc/sing-box/config.json"
DEFAULT_REPO="sihasiha/vpn_view"
INSTALL_ID="$(date +%Y%m%d%H%M%S%N)"
MANIFEST_FILE="${ETC_DIR}/install-manifest-${INSTALL_ID}.json"
ROLLBACK_ON_FAILURE=0

VPNVIEW_REPO="${VPNVIEW_REPO:-$DEFAULT_REPO}"
VPNVIEW_VERSION="${VPNVIEW_VERSION:-latest}"
VPNVIEW_BIN="${VPNVIEW_BIN:-}"
VPNVIEW_CLIENT_BIN="${VPNVIEW_CLIENT_BIN:-}"
VPNVIEW_CLIENT_CONFIG="${VPNVIEW_CLIENT_CONFIG:-}"
VPNVIEW_CLIENT_SERVICE="${VPNVIEW_CLIENT_SERVICE:-}"
VPNVIEW_PROTOCOL="${VPNVIEW_PROTOCOL:-}"
VPNVIEW_MODE="${VPNVIEW_MODE:-takeover}"
SINGBOX_BIN="${SINGBOX_BIN:-}"
SKIP_DOWNLOAD="${SKIP_DOWNLOAD:-0}"

COLOR_RESET=""
COLOR_RED=""
COLOR_GREEN=""
COLOR_YELLOW=""
COLOR_BLUE=""
if [ -t 1 ]; then
  COLOR_RESET="$(printf '\033[0m')"
  COLOR_RED="$(printf '\033[31m')"
  COLOR_GREEN="$(printf '\033[32m')"
  COLOR_YELLOW="$(printf '\033[33m')"
  COLOR_BLUE="$(printf '\033[34m')"
fi

log() { printf '%s[INFO]%s %s\n' "$COLOR_BLUE" "$COLOR_RESET" "$*"; }
ok() { printf '%s[OK]%s %s\n' "$COLOR_GREEN" "$COLOR_RESET" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$COLOR_YELLOW" "$COLOR_RESET" "$*"; }
die() { printf '%s[ERROR]%s %s\n' "$COLOR_RED" "$COLOR_RESET" "$*" >&2; exit 1; }

usage() {
  printf '%s\n' \
    "VPNView one-click installer" \
    "" \
    "Usage:" \
    "  sudo bash install.sh" \
    "  sudo bash install.sh --local ./vpnview-linux-amd64" \
    "  sudo bash install.sh --protocol singbox --mode takeover" \
    "  sudo env VPNVIEW_PROTOCOL=singbox bash install.sh" \
    "  curl -fsSL https://raw.githubusercontent.com/<owner>/vpn_view/main/install.sh | sudo env VPNVIEW_REPO=<owner>/vpn_view bash" \
    "" \
    "Options:" \
    "  --local PATH       Install from a local VPNView binary." \
    "  --repo OWNER/REPO  GitHub repository used for online release download." \
    "  --version TAG      Release tag. Defaults to latest." \
    "  --protocol NAME    VPN core/protocol to manage: singbox, xray, mihomo, hysteria2, tuic, stub." \
    "  --mode MODE        takeover, panel-only, or dry-run. Defaults to takeover." \
    "  --skip-download    Do not download a binary; require an existing local binary." \
    "  -h, --help         Show this help." \
    "" \
    "Environment:" \
    "  VPNVIEW_REPO       GitHub repository, for example owner/vpn_view." \
    "  VPNVIEW_VERSION    Release tag, or latest." \
    "  VPNVIEW_BIN        Local VPNView binary path." \
    "  VPNVIEW_CLIENT_BIN Override the managed VPN client binary path." \
    "  VPNVIEW_CLIENT_CONFIG Override the managed VPN client config path." \
    "  VPNVIEW_CLIENT_SERVICE Override the managed VPN client systemd service name." \
    "  VPNVIEW_PROTOCOL   Non-interactive protocol selection." \
    "  VPNVIEW_MODE       takeover, panel-only, or dry-run." \
    "  SINGBOX_BIN        sing-box binary path if it is not in PATH."
}

while [ $# -gt 0 ]; do
  case "$1" in
    --local)
      [ $# -ge 2 ] || die "--local requires a path"
      VPNVIEW_BIN="$2"
      shift 2
      ;;
    --repo)
      [ $# -ge 2 ] || die "--repo requires OWNER/REPO"
      VPNVIEW_REPO="$2"
      shift 2
      ;;
    --version)
      [ $# -ge 2 ] || die "--version requires a release tag"
      VPNVIEW_VERSION="$2"
      shift 2
      ;;
    --protocol)
      [ $# -ge 2 ] || die "--protocol requires a protocol name"
      VPNVIEW_PROTOCOL="$2"
      shift 2
      ;;
    --mode)
      [ $# -ge 2 ] || die "--mode requires takeover, panel-only, or dry-run"
      VPNVIEW_MODE="$2"
      shift 2
      ;;
    --skip-download)
      SKIP_DOWNLOAD=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "please run this script as root, for example: sudo bash install.sh"
  fi
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

detect_arch() {
  local machine
  machine="$(uname -m)"
  case "$machine" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    armv7l|armv7) printf 'armv7' ;;
    *) die "unsupported architecture: ${machine}" ;;
  esac
}

script_dir() {
  local source="${BASH_SOURCE[0]:-$0}"
  if [ -f "$source" ]; then
    cd -- "$(dirname -- "$source")" >/dev/null 2>&1 && pwd
  else
    pwd
  fi
}

backup_file() {
  local path="$1"
  if [ -e "$path" ]; then
    local backup="${path}.bak.$(date +%Y%m%d%H%M%S%N)"
    cp -a "$path" "$backup"
    record_manifest_file "$path" "$backup" "replace"
    log "backed up ${path} to ${backup}"
  else
    record_manifest_file "$path" "" "create"
  fi
}

init_manifest() {
  mkdir -p "$(dirname "$MANIFEST_FILE")"
  python3 - "$MANIFEST_FILE" "$INSTALL_ID" <<'PY'
import json
import re
import sys

path, install_id = sys.argv[1:]
data = {
    "install_id": install_id,
    "files": [],
    "dirs": [],
    "temps": [],
    "services": [],
}
with open(path, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY
  ok "created install manifest: ${MANIFEST_FILE}"
}

record_manifest_file() {
  local path="$1"
  local backup="$2"
  local action="$3"
  if [ -z "${MANIFEST_FILE:-}" ] || [ ! -f "$MANIFEST_FILE" ]; then
    return 0
  fi
  python3 - "$MANIFEST_FILE" "$path" "$backup" "$action" <<'PY'
import json
import sys

manifest, path, backup, action = sys.argv[1:]
with open(manifest, "r", encoding="utf-8") as f:
    data = json.load(f)
entry = {"path": path, "backup": backup, "action": action}
if entry not in data.setdefault("files", []):
    data["files"].append(entry)
with open(manifest, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY
}

record_manifest_dir() {
  local path="$1"
  if [ -z "${MANIFEST_FILE:-}" ] || [ ! -f "$MANIFEST_FILE" ]; then
    return 0
  fi
  python3 - "$MANIFEST_FILE" "$path" <<'PY'
import json
import sys

manifest, path = sys.argv[1:]
with open(manifest, "r", encoding="utf-8") as f:
    data = json.load(f)
if path not in data.setdefault("dirs", []):
    data["dirs"].append(path)
with open(manifest, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY
}

record_manifest_temp() {
  local path="$1"
  if [ -z "${MANIFEST_FILE:-}" ] || [ ! -f "$MANIFEST_FILE" ]; then
    return 0
  fi
  python3 - "$MANIFEST_FILE" "$path" <<'PY'
import json
import sys

manifest, path = sys.argv[1:]
with open(manifest, "r", encoding="utf-8") as f:
    data = json.load(f)
if path not in data.setdefault("temps", []):
    data["temps"].append(path)
with open(manifest, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY
}

record_manifest_service() {
  local service="$1"
  if [ -z "$service" ] || [ -z "${MANIFEST_FILE:-}" ] || [ ! -f "$MANIFEST_FILE" ]; then
    return 0
  fi
  command -v systemctl >/dev/null 2>&1 || return 0
  local enabled
  local active
  enabled="$(systemctl is-enabled "$service" 2>/dev/null || true)"
  active="$(systemctl is-active "$service" 2>/dev/null || true)"
  python3 - "$MANIFEST_FILE" "$service" "$enabled" "$active" <<'PY'
import json
import sys

manifest, service, enabled, active = sys.argv[1:]
with open(manifest, "r", encoding="utf-8") as f:
    data = json.load(f)
services = data.setdefault("services", [])
if not any(item.get("name") == service for item in services):
    services.append({"name": service, "enabled": enabled, "active": active})
with open(manifest, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
PY
}

rollback_install() {
  if [ "$ROLLBACK_ON_FAILURE" != "1" ] || [ ! -f "$MANIFEST_FILE" ]; then
    return 0
  fi
  warn "install failed; restoring files from ${MANIFEST_FILE}"
  python3 - "$MANIFEST_FILE" <<'PY'
import json
import os
import shutil
import sys

manifest = sys.argv[1]
with open(manifest, "r", encoding="utf-8") as f:
    data = json.load(f)
for item in reversed(data.get("files", [])):
    src = item.get("backup", "")
    dst = item.get("path", "")
    action = item.get("action", "")
    if action == "create":
        if dst and os.path.exists(dst):
            if os.path.isdir(dst):
                shutil.rmtree(dst)
            else:
                os.remove(dst)
            print(f"removed created {dst}")
        continue
    if not src or not dst or not os.path.exists(src):
        continue
    os.makedirs(os.path.dirname(dst) or ".", exist_ok=True)
    if os.path.isdir(src):
        if os.path.exists(dst):
            shutil.rmtree(dst)
        shutil.copytree(src, dst, symlinks=True)
    else:
        shutil.copy2(src, dst)
        shutil.copystat(src, dst)
    print(f"restored {dst} from {src}")
for temp in data.get("temps", []):
    if temp and os.path.exists(temp):
        if os.path.isdir(temp):
            shutil.rmtree(temp)
        else:
            os.remove(temp)
        print(f"removed temp {temp}")
for directory in reversed(data.get("dirs", [])):
    if directory and os.path.isdir(directory):
        try:
            os.rmdir(directory)
            print(f"removed empty dir {directory}")
        except OSError:
            pass
PY
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
    python3 - "$MANIFEST_FILE" <<'PY' | while IFS="$(printf '\t')" read -r name enabled active; do
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as f:
    data = json.load(f)
for item in reversed(data.get("services", [])):
    print("{}\t{}\t{}".format(item.get("name", ""), item.get("enabled", ""), item.get("active", "")))
PY
      [ -n "$name" ] || continue
      case "$enabled" in
        enabled) systemctl enable "$name" >/dev/null 2>&1 || true ;;
        disabled) systemctl disable "$name" >/dev/null 2>&1 || true ;;
      esac
      case "$active" in
        active) systemctl start "$name" >/dev/null 2>&1 || true ;;
        inactive|failed|unknown|"") systemctl stop "$name" >/dev/null 2>&1 || true ;;
      esac
    done
  fi
}

on_exit() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    rollback_install
  fi
}
trap on_exit EXIT

find_local_binary() {
  local arch="$1"
  local dir
  dir="$(script_dir)"
  if [ -n "$VPNVIEW_BIN" ] && [ -f "$VPNVIEW_BIN" ]; then
    printf '%s' "$VPNVIEW_BIN"
    return 0
  fi
  for candidate in \
    "./vpnview-linux-${arch}" \
    "./vpnview" \
    "${dir}/vpnview-linux-${arch}" \
    "${dir}/vpnview"; do
    if [ -f "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

download_binary() {
  local arch="$1"
  local output="$2"
  local url
  if [ "$VPNVIEW_VERSION" = "latest" ]; then
    url="https://github.com/${VPNVIEW_REPO}/releases/latest/download/vpnview-linux-${arch}"
  else
    url="https://github.com/${VPNVIEW_REPO}/releases/download/${VPNVIEW_VERSION}/vpnview-linux-${arch}"
  fi
  log "downloading VPNView binary from ${url}"
  curl -fL --retry 3 --connect-timeout 15 -o "$output" "$url" || die "failed to download ${url}; check VPNVIEW_REPO, VPNVIEW_VERSION, and release asset name"
}

install_binary() {
  local arch="$1"
  local tmp
  local local_path
  tmp="$(mktemp)"
  if local_path="$(find_local_binary "$arch")"; then
    log "using local VPNView binary: ${local_path}"
    cp "$local_path" "$tmp"
  else
    [ "$SKIP_DOWNLOAD" = "0" ] || die "no local binary found and --skip-download is set"
    require_command curl
    download_binary "$arch" "$tmp"
  fi

  backup_file "$INSTALL_BIN"
  install -m 0755 "$tmp" "$INSTALL_BIN"
  rm -f "$tmp"
  ok "installed VPNView to ${INSTALL_BIN}"
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    python3 - <<'PY'
import secrets
print(secrets.token_hex(24))
PY
  fi
}

yaml_get() {
  local path="$1"
  local key_path="$2"
  python3 - "$path" "$key_path" <<'PY'
import re
import sys

path, key_path = sys.argv[1], sys.argv[2]
keys = key_path.split(".")
stack = []
try:
    lines = open(path, "r", encoding="utf-8").read().splitlines()
except FileNotFoundError:
    sys.exit(1)

for line in lines:
    if not line.strip() or line.lstrip().startswith("#"):
        continue
    indent = len(line) - len(line.lstrip(" "))
    level = indent // 2
    text = line.strip()
    match = re.match(r"([^:#]+):(?:\s*(.*))?$", text)
    if not match:
        continue
    key = match.group(1).strip()
    value = (match.group(2) or "").strip()
    stack = stack[:level]
    stack.append(key)
    if stack == keys and value != "":
        if (value.startswith('"') and value.endswith('"')) or (value.startswith("'") and value.endswith("'")):
            value = value[1:-1]
        print(value)
        sys.exit(0)
sys.exit(1)
PY
}

create_default_config() {
  local secret="$1"
  cat > "$CONFIG_FILE" <<EOF
server:
  listen: "0.0.0.0:19463"

auth:
  secret: "${secret}"
  token_ttl: "24h"

adapter:
  type: "singbox"
  config_template_path: "${TEMPLATE_FILE}"
  singbox_config_path: "${DEFAULT_SINGBOX_CONFIG}"
  inbound_tag: ""
  clash_api: "http://127.0.0.1:9090"
  clash_secret: ""
  v2ray_api: "127.0.0.1:10085"

cores:
  default: "singbox-main"
  enabled: ["singbox-main"]
  items:
    singbox-main:
      type: "singbox"
      enabled: true
      role: "primary"
      config:
        config_template_path: "${TEMPLATE_FILE}"
        singbox_config_path: "${DEFAULT_SINGBOX_CONFIG}"
        inbound_tag: ""
        clash_api: "http://127.0.0.1:9090"
        clash_secret: ""
        v2ray_api: "127.0.0.1:10085"

store:
  sqlite:
    path: "${DATA_DIR}/vpnview.db"

limits:
  global_upload_speed: 0
  global_download_speed: 0
  default_user_upload_speed: 0
  default_user_download_speed: 0
  default_quota: 0
  software_limit_strikes: 3

subscription:
  mode: "link"
  domain: ""
  template_path: ""

poll_interval: "5s"
EOF
}

patch_config() {
  local adapter_type="$1"
  local template_path="$2"
  local client_config_key="$3"
  local client_config_path="$4"
  python3 - "$CONFIG_FILE" "$adapter_type" "$template_path" "$client_config_key" "$client_config_path" "$DATA_DIR/vpnview.db" <<'PY'
import sys

path, adapter_type, template_path, client_config_key, client_config_path, db_path = sys.argv[1:]

try:
    lines = open(path, "r", encoding="utf-8").read().splitlines()
except FileNotFoundError:
    lines = []

def section_bounds(lines, section):
    start = None
    for i, line in enumerate(lines):
        if line.startswith(section + ":"):
            start = i
            break
    if start is None:
        return None, None
    end = len(lines)
    for j in range(start + 1, len(lines)):
        if lines[j] and not lines[j].startswith((" ", "\t")) and lines[j].rstrip().endswith(":"):
            end = j
            break
    return start, end

def set_section_key(lines, section, key, value):
    start, end = section_bounds(lines, section)
    rendered = f'  {key}: "{value}"'
    if start is None:
        if lines and lines[-1] != "":
            lines.append("")
        lines.extend([f"{section}:", rendered])
        return lines
    for i in range(start + 1, end):
        stripped = lines[i].strip()
        if lines[i].startswith("  ") and not lines[i].startswith("    ") and stripped.startswith(key + ":"):
            lines[i] = rendered
            return lines
    lines.insert(end, rendered)
    return lines

def ensure_store_sqlite_path(lines, value):
    start, end = section_bounds(lines, "store")
    if start is None:
        if lines and lines[-1] != "":
            lines.append("")
        lines.extend(["store:", "  sqlite:", f'    path: "{value}"'])
        return lines

    sqlite_start = None
    sqlite_end = end
    for i in range(start + 1, end):
        if lines[i].startswith("  sqlite:"):
            sqlite_start = i
            for j in range(i + 1, end):
                if lines[j].startswith("  ") and not lines[j].startswith("    ") and lines[j].strip().endswith(":"):
                    sqlite_end = j
                    break
            break
    if sqlite_start is None:
        lines.insert(end, "  sqlite:")
        lines.insert(end + 1, f'    path: "{value}"')
        return lines

    for i in range(sqlite_start + 1, sqlite_end):
        if lines[i].startswith("    ") and lines[i].strip().startswith("path:"):
            lines[i] = f'    path: "{value}"'
            return lines
    lines.insert(sqlite_end, f'    path: "{value}"')
    return lines

def replace_existing_key_anywhere(lines, key, value):
    if not key:
        return lines
    out = []
    pattern = re.compile(r"^(\s*)" + re.escape(key) + r":\s*.*$")
    for line in lines:
        match = pattern.match(line)
        if match:
            out.append(f'{match.group(1)}{key}: "{value}"')
        else:
            out.append(line)
    return out

lines = set_section_key(lines, "adapter", "type", adapter_type)
if adapter_type.lower() not in {"stub", ""}:
    lines = set_section_key(lines, "adapter", "config_template_path", template_path)
    if client_config_key:
        lines = set_section_key(lines, "adapter", client_config_key, client_config_path)
    if adapter_type.lower() in {"singbox", "sing-box"}:
        lines = set_section_key(lines, "adapter", "inbound_tag", "")
    lines = replace_existing_key_anywhere(lines, "config_template_path", template_path)
    if client_config_key:
        lines = replace_existing_key_anywhere(lines, client_config_key, client_config_path)
lines = ensure_store_sqlite_path(lines, db_path)

with open(path, "w", encoding="utf-8", newline="\n") as f:
    f.write("\n".join(lines).rstrip() + "\n")
PY
}

ensure_directories() {
  if [ ! -d "$ETC_DIR" ]; then
    record_manifest_dir "$ETC_DIR"
  fi
  mkdir -p "$ETC_DIR"
  if [ ! -d "$DATA_DIR" ]; then
    record_manifest_dir "$DATA_DIR"
  fi
  mkdir -p "$DATA_DIR"
  local singbox_dir
  singbox_dir="$(dirname "$DEFAULT_SINGBOX_CONFIG")"
  if [ ! -d "$singbox_dir" ]; then
    record_manifest_dir "$singbox_dir"
  fi
  mkdir -p "$singbox_dir"
  chmod 0755 "$ETC_DIR" "$DATA_DIR"
  ok "initialized ${ETC_DIR} and ${DATA_DIR}"
}

ensure_config() {
  if [ ! -f "$CONFIG_FILE" ]; then
    log "creating default ${CONFIG_FILE}"
    backup_file "$CONFIG_FILE"
    create_default_config "$(random_secret)"
    ok "created ${CONFIG_FILE}; the login secret was generated automatically"
  else
    backup_file "$CONFIG_FILE"
  fi
}

locate_singbox_config() {
  local configured="${1:-}"
  if [ -n "$configured" ] && [ -f "$configured" ]; then
    printf '%s' "$configured"
    return 0
  fi
  if [ -n "$configured" ] && [ "${configured#/}" != "$configured" ]; then
    printf '%s' "$configured"
    return 0
  fi
  for candidate in \
    "/etc/sing-box/config.json" \
    "/etc/singbox/config.json" \
    "/usr/local/etc/sing-box/config.json"; do
    if [ -f "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  if [ -n "$configured" ]; then
    printf '%s' "$configured"
  else
    printf '%s' "$DEFAULT_SINGBOX_CONFIG"
  fi
}

normalize_protocol() {
  local protocol
  protocol="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr '_' '-')"
  case "$protocol" in
    singbox|sing-box) printf 'singbox' ;;
    xray|xray-core) printf 'xray' ;;
    clash|clash-meta|mihomo) printf 'mihomo' ;;
    hysteria|hysteria2) printf 'hysteria2' ;;
    tuic) printf 'tuic' ;;
    stub) printf 'stub' ;;
    *) return 1 ;;
  esac
}

choose_protocol() {
  local raw="${VPNVIEW_PROTOCOL:-}"
  if [ -n "$raw" ]; then
    normalize_protocol "$raw" || die "unsupported protocol: ${raw}"
    return 0
  fi
  if [ ! -t 0 ]; then
    die "non-interactive install requires --protocol NAME or VPNVIEW_PROTOCOL=NAME; refusing to silently default to singbox"
  fi

  printf '%s\n' \
    "Please select the VPN core/protocol to manage:" \
    "  1) singbox" \
    "  2) xray" \
    "  3) mihomo" \
    "  4) hysteria2" \
    "  5) tuic" \
    "  6) stub" >&2
  printf 'Enter number or name: ' >&2
  read -r raw
  case "$raw" in
    1) raw="singbox" ;;
    2) raw="xray" ;;
    3) raw="mihomo" ;;
    4) raw="hysteria2" ;;
    5) raw="tuic" ;;
    6) raw="stub" ;;
  esac
  normalize_protocol "$raw" || die "unsupported protocol: ${raw}"
}

normalize_mode() {
  local mode
  mode="$(printf '%s' "${1:-takeover}" | tr '[:upper:]' '[:lower:]')"
  case "$mode" in
    takeover|panel-only|dry-run) printf '%s' "$mode" ;;
    *) return 1 ;;
  esac
}

print_install_plan() {
  local mode="$1"
  local protocol="$2"
  local adapter_type="$3"
  local core_id="$4"
  local client_service="${5:-}"
  local client_config="${6:-}"
  local template_path="${7:-}"
  local client_bin="${8:-}"
  python3 - "$mode" "$protocol" "$adapter_type" "$core_id" "$client_service" "$client_config" "$template_path" "$client_bin" <<'PY'
import json
import sys

mode, protocol, adapter_type, core_id, client_service, client_config, template_path, client_bin = sys.argv[1:]
print(json.dumps({
    "mode": mode,
    "protocol": protocol,
    "adapter_type": adapter_type,
    "core_id": core_id,
    "converter": "singbox-json" if protocol == "singbox" else "",
    "config_format": "json" if protocol in {"singbox", "xray", "tuic"} else ("yaml" if protocol in {"mihomo", "hysteria2"} else "none"),
    "client_service": client_service,
    "client_binary": client_bin,
    "runtime_config": client_config,
    "template_config": template_path,
    "vpnview_config": "/etc/vpnview/config.yaml",
    "vpnview_binary": "/usr/local/bin/vpnview",
}, ensure_ascii=False, indent=2))
PY
}

adapter_config_key() {
  local adapter_type="$1"
  case "$adapter_type" in
    singbox) printf 'singbox_config_path' ;;
    *) printf '%s_config_path' "$(printf '%s' "$adapter_type" | tr '-' '_')" ;;
  esac
}

client_service_name() {
  local adapter_type="$1"
  if [ -n "$VPNVIEW_CLIENT_SERVICE" ]; then
    printf '%s' "$VPNVIEW_CLIENT_SERVICE"
    return 0
  fi
  case "$adapter_type" in
    singbox) printf 'sing-box' ;;
    *) printf '%s' "$adapter_type" ;;
  esac
}

client_binary_name() {
  local adapter_type="$1"
  case "$adapter_type" in
    singbox) printf 'sing-box' ;;
    *) printf '%s' "$adapter_type" ;;
  esac
}

template_file_for_adapter() {
  local adapter_type="$1"
  printf '%s/%s_template.json' "$ETC_DIR" "$(printf '%s' "$adapter_type" | tr '-' '_')"
}

discover_config_from_systemd() {
  local adapter_type="$1"
  local service_name="$2"
  command -v systemctl >/dev/null 2>&1 || return 1
  local unit
  unit="$(systemctl cat "${service_name}.service" 2>/dev/null || true)"
  [ -n "$unit" ] || return 1
  printf '%s\n' "$unit" | python3 - "$adapter_type" <<'PY'
import os
import shlex
import sys

adapter = sys.argv[1]
text = sys.stdin.read()

def from_dir(directory):
    if not directory:
        return ""
    filename = "config.yaml" if adapter in {"mihomo", "clash", "hysteria", "hysteria2"} else "config.json"
    return os.path.join(directory, filename)

for raw in text.splitlines():
    line = raw.strip()
    if not line.startswith("ExecStart="):
        continue
    command = line.split("=", 1)[1].strip()
    if not command:
        continue
    try:
        parts = shlex.split(command)
    except ValueError:
        continue
    for index, part in enumerate(parts):
        if part in {"-c", "-config", "--config", "-f"} and index + 1 < len(parts):
            print(parts[index + 1])
            sys.exit(0)
        if part in {"-d", "--directory"} and index + 1 < len(parts):
            print(from_dir(parts[index + 1]))
            sys.exit(0)
        for prefix in ("-c=", "-config=", "--config=", "-f="):
            if part.startswith(prefix):
                print(part.split("=", 1)[1])
                sys.exit(0)
        for prefix in ("-d=", "--directory="):
            if part.startswith(prefix):
                print(from_dir(part.split("=", 1)[1]))
                sys.exit(0)
        if part.endswith((".json", ".yaml", ".yml")) and part.startswith("/"):
            print(part)
            sys.exit(0)
sys.exit(1)
PY
}

locate_client_config() {
  local adapter_type="$1"
  local service_name="$2"
  local configured="${3:-}"
  local discovered=""

  if [ -n "$VPNVIEW_CLIENT_CONFIG" ]; then
    printf '%s' "$VPNVIEW_CLIENT_CONFIG"
    return 0
  fi
  if [ "$adapter_type" = "singbox" ]; then
    locate_singbox_config "$configured"
    return 0
  fi
  if [ -n "$configured" ] && [ -f "$configured" ]; then
    printf '%s' "$configured"
    return 0
  fi
  if [ -n "$configured" ] && [ "${configured#/}" != "$configured" ]; then
    printf '%s' "$configured"
    return 0
  fi
  discovered="$(discover_config_from_systemd "$adapter_type" "$service_name" || true)"
  if [ -n "$discovered" ]; then
    printf '%s' "$discovered"
    return 0
  fi
  for candidate in \
    "/etc/${service_name}/config.json" \
    "/etc/${adapter_type}/config.json" \
    "/usr/local/etc/${service_name}/config.json" \
    "/usr/local/etc/${adapter_type}/config.json"; do
    if [ -f "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  printf '/etc/%s/config.json' "$service_name"
}

create_minimal_singbox_template() {
  cat > "$TEMPLATE_FILE" <<'EOF'
{
  "log": {
    "level": "info"
  },
  "experimental": {
    "clash_api": {
      "external_controller": "127.0.0.1:9090",
      "secret": ""
    },
    "v2ray_api": {
      "listen": "127.0.0.1:10085",
      "stats": {
        "enabled": true,
        "inbounds": [
          "vless-in"
        ]
      }
    }
  },
  "inbounds": [
    {
      "type": "vless",
      "tag": "vless-in",
      "listen": "::",
      "listen_port": 1443,
      "users": []
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "auto_detect_interface": true
  }
}
EOF
}

generate_template_from_client_config() {
  local adapter_type="$1"
  local source="$2"
  local template_path="$3"
  case "$adapter_type" in
    singbox) generate_template_from_singbox_config "$source" "$template_path" ;;
    *) die "takeover converter for ${adapter_type} is not implemented yet; rerun with --mode panel-only" ;;
  esac
}

generate_template_from_singbox_config() {
  local source="$1"
  local template_path="${2:-$TEMPLATE_FILE}"
  if [ -f "$source" ]; then
    backup_file "$template_path"
    python3 - "$source" "$template_path" <<'PY'
import json
import sys

source, output = sys.argv[1], sys.argv[2]
with open(source, "r", encoding="utf-8") as f:
    data = json.load(f)

cleared = 0

def clear_inbound_users(node):
    global cleared
    if isinstance(node, dict):
        inbounds = node.get("inbounds")
        if isinstance(inbounds, list):
            for inbound in inbounds:
                if not isinstance(inbound, dict):
                    continue
                for key in ("users", "clients"):
                    if isinstance(inbound.get(key), list):
                        inbound[key] = []
                        cleared += 1
                settings = inbound.get("settings")
                if isinstance(settings, dict):
                    for key in ("users", "clients"):
                        if isinstance(settings.get(key), list):
                            settings[key] = []
                            cleared += 1
        for value in node.values():
            clear_inbound_users(value)
    elif isinstance(node, list):
        for item in node:
            clear_inbound_users(item)

clear_inbound_users(data)
with open(output, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
print(cleared)
PY
    ok "generated ${template_path} from ${source} with inbound users/clients cleared"
  else
    if [ "$template_path" = "$TEMPLATE_FILE" ]; then
      warn "sing-box config was not found at ${source}; creating a minimal template"
      backup_file "$template_path"
      create_minimal_singbox_template
      ok "created ${template_path}"
    else
      die "client config was not found at ${source}; create it first or set VPNVIEW_CLIENT_CONFIG=/path/to/config.json"
    fi
  fi
}

write_init_config() {
  local source_config="$1"
  local init_config="$2"
  local template_path="$3"
  local client_config_key="$4"
  local staging_config_path="$5"
  python3 - "$source_config" "$init_config" "$template_path" "$client_config_key" "$staging_config_path" <<'PY'
import re
import sys

source, output, template_path, client_key, staging_path = sys.argv[1:]
lines = open(source, "r", encoding="utf-8").read().splitlines()

def replace_value(line, key, value):
    match = re.match(r"^(\s*)" + re.escape(key) + r":\s*.*$", line)
    if not match:
        return None
    return f'{match.group(1)}{key}: "{value}"'

out = []
for line in lines:
    replaced = replace_value(line, "config_template_path", template_path)
    if replaced is None and client_key:
        replaced = replace_value(line, client_key, staging_path)
    out.append(replaced if replaced is not None else line)

with open(output, "w", encoding="utf-8", newline="\n") as f:
    f.write("\n".join(out).rstrip() + "\n")
PY
}

commit_staged_client_config() {
  local staging_path="$1"
  local runtime_path="$2"
  if [ ! -s "$staging_path" ]; then
    die "staged client config is missing or empty: ${staging_path}"
  fi
  backup_file "$runtime_path"
  mkdir -p "$(dirname "$runtime_path")"
  mv -f "$staging_path" "$runtime_path"
  ok "replaced ${runtime_path} with validated staged config"
}

find_client_binary() {
  local adapter_type="$1"
  local service_name="$2"
  local binary_name
  binary_name="$(client_binary_name "$adapter_type")"

  if [ -n "$VPNVIEW_CLIENT_BIN" ] && [ -x "$VPNVIEW_CLIENT_BIN" ]; then
    printf '%s' "$VPNVIEW_CLIENT_BIN"
    return 0
  fi
  if [ "$adapter_type" = "singbox" ] && [ -n "$SINGBOX_BIN" ] && [ -x "$SINGBOX_BIN" ]; then
    printf '%s' "$SINGBOX_BIN"
    return 0
  fi
  if command -v "$binary_name" >/dev/null 2>&1; then
    command -v "$binary_name"
    return 0
  fi
  if command -v "$service_name" >/dev/null 2>&1; then
    command -v "$service_name"
    return 0
  fi
  for candidate in "/usr/local/bin/${binary_name}" "/usr/bin/${binary_name}" "/bin/${binary_name}" "/usr/local/bin/${service_name}" "/usr/bin/${service_name}"; do
    if [ -x "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

write_vpnview_service() {
  local client_service="${1:-}"
  local after="network-online.target"
  local wants="network-online.target"
  if [ -n "$client_service" ]; then
    after="${after} ${client_service}.service"
    wants="${wants} ${client_service}.service"
  fi
  backup_file "/etc/systemd/system/${SERVICE_NAME}.service"
  cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=VPNView Admin Panel
After=${after}
Wants=${wants}

[Service]
Type=simple
ExecStart=${INSTALL_BIN} -config ${CONFIG_FILE}
WorkingDirectory=${ETC_DIR}
Restart=always
RestartSec=5s
StartLimitIntervalSec=60
StartLimitBurst=10
LimitNOFILE=1048576
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
  ok "wrote /etc/systemd/system/${SERVICE_NAME}.service"
}

client_exec_args() {
  local adapter_type="$1"
  case "$adapter_type" in
    singbox) printf 'run -c' ;;
    *) printf 'run -config' ;;
  esac
}

write_client_service() {
  local adapter_type="$1"
  local client_service="$2"
  local client_bin="$3"
  local client_config_path="$4"
  local exec_args
  exec_args="$(client_exec_args "$adapter_type")"
  local exec_start_pre=""
  if [ "$adapter_type" = "singbox" ]; then
    exec_start_pre="ExecStartPre=${client_bin} check -c ${client_config_path}"
  fi
  backup_file "/etc/systemd/system/${client_service}.service"
  cat > "/etc/systemd/system/${client_service}.service" <<EOF
[Unit]
Description=${client_service} service
After=network-online.target nss-lookup.target
Wants=network-online.target

[Service]
Type=simple
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW
${exec_start_pre}
ExecStart=${client_bin} ${exec_args} ${client_config_path}
Restart=always
RestartSec=5s
StartLimitIntervalSec=60
StartLimitBurst=10
LimitNOFILE=1048576
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
  ok "wrote /etc/systemd/system/${client_service}.service"
}

stop_existing_services() {
  local client_service="${1:-}"
  if command -v systemctl >/dev/null 2>&1; then
    record_manifest_service "$SERVICE_NAME"
    systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
    if [ -n "$client_service" ]; then
      record_manifest_service "$client_service"
      systemctl stop "$client_service" >/dev/null 2>&1 || true
    fi
  fi
}

run_initial_vpnview_once() {
  local config_path="${1:-$CONFIG_FILE}"
  log "running VPNView briefly to generate the first client config"
  local log_file="/tmp/vpnview-install-init.log"
  set +e
  "$INSTALL_BIN" -config "$config_path" >"$log_file" 2>&1 &
  local pid=$!
  sleep 3
  if kill -0 "$pid" >/dev/null 2>&1; then
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
    set -e
    ok "initial VPNView run completed"
    return 0
  fi
  wait "$pid"
  local code=$?
  set -e
  if [ "$code" -ne 0 ]; then
    warn "VPNView exited during initial run. Recent log:"
    tail -n 40 "$log_file" || true
    die "initial VPNView run failed"
  fi
  ok "initial VPNView run completed"
}

validate_client_config() {
  local adapter_type="$1"
  local client_bin="$2"
  local client_config_path="$3"
  if [ ! -s "$client_config_path" ]; then
    die "generated client config is missing or empty: ${client_config_path}"
  fi
  case "$adapter_type" in
    singbox)
      "$client_bin" check -c "$client_config_path" || die "sing-box config validation failed: ${client_config_path}"
      ok "validated sing-box config: ${client_config_path}"
      ;;
    *)
      warn "no config validator is implemented for ${adapter_type}"
      ;;
  esac
}

systemd_start() {
  local client_service="${1:-}"
  require_command systemctl
  if ! systemctl >/dev/null 2>&1; then
    die "systemd is not available on this system"
  fi

  stop_existing_services "$client_service"
  systemctl daemon-reload

  if [ -n "$client_service" ]; then
    record_manifest_service "$client_service"
    systemctl enable "$client_service" >/dev/null
    systemctl start "$client_service"
  fi
  record_manifest_service "$SERVICE_NAME"
  systemctl enable "$SERVICE_NAME" >/dev/null
  systemctl start "$SERVICE_NAME"

  if [ -n "$client_service" ]; then
    systemctl is-active --quiet "$client_service" || {
      journalctl -u "$client_service" -n 40 --no-pager || true
      die "${client_service}.service is not active"
    }
  fi
  systemctl is-active --quiet "$SERVICE_NAME" || {
    journalctl -u "$SERVICE_NAME" -n 40 --no-pager || true
    die "${SERVICE_NAME}.service is not active"
  }
  ok "services are enabled and running"
}

main() {
  require_command uname
  require_command python3

  local arch
  arch="$(detect_arch)"
  log "detected architecture: ${arch}"

  local adapter_type
  local install_mode
  adapter_type="$(choose_protocol)"
  install_mode="$(normalize_mode "$VPNVIEW_MODE")" || die "unsupported install mode: ${VPNVIEW_MODE}"
  log "protocol: ${adapter_type}"
  log "install mode: ${install_mode}"
  if [ "$install_mode" = "takeover" ] && [ "$adapter_type" != "singbox" ]; then
    die "takeover for ${adapter_type} is not production-ready yet; rerun with --mode panel-only"
  fi

  local client_config_path=""
  local client_template_path=""
  local client_service=""
  local client_bin=""
  local config_key=""
  if [ "$adapter_type" != "stub" ] && [ "$install_mode" != "panel-only" ]; then
    local configured_path=""
    config_key="$(adapter_config_key "$adapter_type")"
    if [ -f "$CONFIG_FILE" ]; then
      configured_path="$(yaml_get "$CONFIG_FILE" "adapter.${config_key}" 2>/dev/null || true)"
    fi
    client_service="$(client_service_name "$adapter_type")"
    client_config_path="$(locate_client_config "$adapter_type" "$client_service" "$configured_path")"
    client_template_path="$(template_file_for_adapter "$adapter_type")"
    client_bin="$(find_client_binary "$adapter_type" "$client_service")" || true
  fi
  print_install_plan "$install_mode" "$adapter_type" "$adapter_type" "${adapter_type}-main" "$client_service" "$client_config_path" "$client_template_path" "$client_bin"
  if [ "$install_mode" = "dry-run" ]; then
    ok "dry-run complete; no files were changed"
    return 0
  fi

  require_root
  require_command install
  require_command mktemp

  mkdir -p "$ETC_DIR"
  init_manifest
  ROLLBACK_ON_FAILURE=1
  ensure_directories
  install_binary "$arch"
  ensure_config

  if [ "$install_mode" = "panel-only" ]; then
    warn "panel-only mode: VPNView will be installed without taking over a VPN core service"
    patch_config "stub" "" "" ""
    write_vpnview_service ""
    systemd_start ""
  elif [ "$adapter_type" != "stub" ]; then
    local configured_path
    config_key="$(adapter_config_key "$adapter_type")"
    configured_path="$(yaml_get "$CONFIG_FILE" "adapter.${config_key}" 2>/dev/null || true)"
    client_service="$(client_service_name "$adapter_type")"
    client_config_path="$(locate_client_config "$adapter_type" "$client_service" "$configured_path")"
    client_template_path="$(template_file_for_adapter "$adapter_type")"
    local client_config_dir
    client_config_dir="$(dirname "$client_config_path")"
    if [ ! -d "$client_config_dir" ]; then
      record_manifest_dir "$client_config_dir"
    fi
    mkdir -p "$client_config_dir"
    generate_template_from_client_config "$adapter_type" "$client_config_path" "$client_template_path"
    patch_config "$adapter_type" "$client_template_path" "$config_key" "$client_config_path"

    client_bin="$(find_client_binary "$adapter_type" "$client_service")" || die "${client_service} binary was not found. Install it first, or rerun with VPNVIEW_CLIENT_BIN=/path/to/${client_service}"
    local staging_config_path="${client_config_path}.vpnview.new"
    record_manifest_temp "$staging_config_path"
    local init_config
    init_config="$(mktemp)"
    record_manifest_temp "$init_config"
    write_init_config "$CONFIG_FILE" "$init_config" "$client_template_path" "$config_key" "$staging_config_path"
    run_initial_vpnview_once "$init_config"
    validate_client_config "$adapter_type" "$client_bin" "$staging_config_path"
    commit_staged_client_config "$staging_config_path" "$client_config_path"
    write_client_service "$adapter_type" "$client_service" "$client_bin" "$client_config_path"
    write_vpnview_service "$client_service"
    stop_existing_services "$client_service"
    systemd_start "$client_service"
  else
    warn "adapter ${adapter_type} does not need a managed VPN client service; only VPNView service will be installed"
    patch_config "$adapter_type" "" "" ""
    write_vpnview_service ""
    systemd_start ""
  fi

  ok "VPNView deployment finished"
  ROLLBACK_ON_FAILURE=0
  printf '\nPanel config: %s\n' "$CONFIG_FILE"
  printf 'Panel service: systemctl status %s\n' "$SERVICE_NAME"
  if [ -n "$client_service" ]; then
    printf 'Template file: %s\n' "$client_template_path"
    printf 'Client config: %s\n' "$client_config_path"
    printf 'Client service: systemctl status %s\n' "$client_service"
  fi
}

main "$@"
