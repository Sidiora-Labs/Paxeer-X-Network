#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
role=
manager=systemd
verify=false
receipt=
env_file=
halt_override=
release_doc=upgrade-info.json

fail() {
  printf 'swap: %s\n' "$*" >&2
  exit 1
}

while (($#)); do
  case "$1" in
    --role|--service-manager|--receipt|--env-file|--halt-height|--release-doc)
      (($# >= 2)) && [[ -n "$2" ]] || fail "missing value for $1"
      case "$1" in
        --role) role=$2 ;;
        --service-manager) manager=$2 ;;
        --receipt) receipt=$2 ;;
        --env-file) env_file=$2 ;;
        --halt-height) halt_override=$2 ;;
        --release-doc) release_doc=$2 ;;
      esac
      shift 2
      ;;
    --verify) verify=true; shift ;;
    *) fail "unknown argument $1" ;;
  esac
done

if [[ -n "$env_file" ]]; then
  [[ -f "$env_file" ]] || fail "env file is missing"
  set -a
  source "$env_file"
  set +a
fi
[[ "$role" == full || "$role" == validator ]] || fail "--role must be full or validator"
[[ "$manager" == systemd || "$manager" == foreground ]] || fail "--service-manager must be systemd or foreground"
halt=${halt_override:-${HALT_HEIGHT:-}}
[[ "$halt" =~ ^[1-9][0-9]*$ ]] || fail "halt height must be a positive integer"
python3 - "$halt" <<'PY' || exit 1
import sys

if int(sys.argv[1]) >= (1 << 63) - 1:
    sys.exit("swap: halt height exceeds the upgrade height range")
PY
[[ -n "${BIN_DIR:-}" && -d "$BIN_DIR" ]] || fail "BIN_DIR must be an existing directory"
[[ "${RELEASE_SHA256:-}" =~ ^[0-9a-f]{64}$ ]] || fail "RELEASE_SHA256 must be 64 lowercase hex digits"
[[ ${VALIDATOR_HOMES+x} ]] || fail "VALIDATOR_HOMES must be declared"
read -r -a validators <<< "$VALIDATOR_HOMES"
if [[ "$role" == validator ]]; then
  homes=("${validators[@]}")
  read -r -a units <<< "${VALIDATOR_UNITS:-}"
else
  read -r -a homes <<< "${FULLNODE_HOMES:-}"
  read -r -a units <<< "${FULLNODE_UNITS:-}"
fi
((${#homes[@]})) || fail "no homes for selected role"
if [[ "$manager" == systemd ]]; then
  ((${#units[@]} == ${#homes[@]})) || fail "homes and units must have the same count"
  for unit in "${units[@]}"; do
    [[ "$unit" != -* ]] || fail "invalid service unit"
  done
fi
for home in "${homes[@]}" "${validators[@]}"; do
  [[ "$home" == /* && -d "$home/data" && -f "$home/config/app.toml" ]] || fail "invalid node home $home"
done
python3 - "${homes[@]}" <<'PY' || exit 1
import os
import sys

homes = [os.path.realpath(home) for home in sys.argv[1:]]
if len(set(homes)) != len(homes):
    sys.exit("swap: duplicate node home")
PY

exec 9>"$BIN_DIR/.paxd-swap.lock"
flock -n 9 || fail "another swap is in progress"
new_owned=false
cleanup() {
  if [[ "$new_owned" == true ]]; then
    rm -f -- "$BIN_DIR/paxd.new"
  fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

check_binary() {
  local binary=$1 linkage status=0
  [[ -f "$binary" && -x "$binary" ]] || fail "paxd binary is missing or not executable"
  printf '%s  %s\n' "$RELEASE_SHA256" "$binary" | sha256sum --check --strict --status || fail "paxd sha256 mismatch"
  linkage=$(ldd "$binary" 2>&1) || status=$?
  if [[ "$linkage" == *'not found'* ]]; then
    fail "unresolved shared library $(awk '/not found/ {print $1; exit}' <<< "$linkage")"
  fi
  ((status == 0)) || fail "ldd could not inspect paxd"
}

node_process() {
  python3 - "$@" <<'PY'
import hashlib
import os
from pathlib import Path
import select
import signal
import subprocess
import sys

mode, home = sys.argv[1:3]
home = os.path.realpath(home)
result = subprocess.run(["pgrep", "-a", "-x", "paxd"], capture_output=True, text=True)
if result.returncode not in (0, 1):
    sys.exit("swap: process inspection failed")
matched = []
for line in result.stdout.splitlines():
    pid = int(line.split(maxsplit=1)[0])
    try:
        raw_args = Path(f"/proc/{pid}/cmdline").read_bytes()
        args = raw_args.split(b"\0")
        args = [os.fsdecode(arg) for arg in args if arg]
        values = []
        for index, arg in enumerate(args):
            if arg == "--home" and index + 1 < len(args):
                values.append(args[index + 1])
            elif arg.startswith("--home="):
                values.append(arg.partition("=")[2])
        for value in values:
            candidate = Path(value)
            if not candidate.is_absolute():
                candidate = Path(f"/proc/{pid}/cwd").resolve() / candidate
            if os.path.realpath(candidate) == home:
                matched.append((pid, raw_args))
                break
    except FileNotFoundError:
        continue
if mode == "list":
    for pid, raw_args in matched:
        print(pid)
elif mode == "stop":
    for pid, raw_args in matched:
        try:
            fd = os.pidfd_open(pid)
        except ProcessLookupError:
            continue
        try:
            current = Path(f"/proc/{pid}/cmdline").read_bytes()
            if current != raw_args:
                sys.exit("swap: process changed during stop")
            signal.pidfd_send_signal(fd, signal.SIGTERM)
            poller = select.poll()
            poller.register(fd, select.POLLIN)
            if not poller.poll(60000):
                sys.exit("swap: node did not stop cleanly")
        finally:
            os.close(fd)
elif mode == "verify":
    if len(matched) != 1:
        sys.exit("swap: expected exactly one running paxd for home")
    with open(f"/proc/{matched[0][0]}/exe", "rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    if digest != sys.argv[3]:
        sys.exit("swap: running paxd sha256 mismatch")
else:
    sys.exit("swap: invalid process operation")
PY
}

check_home() {
  python3 - "$1" "$halt" "$2" <<'PY'
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import tomllib

home, halt, mode = Path(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
path = home / "config/app.toml"
text = path.read_text()
config = tomllib.loads(text)
if type(config.get("halt-height")) is not int:
    sys.exit("swap: app.toml must declare halt-height")
head, separator, tail = text.partition("\n[")
updated, count = re.subn(r"(?m)^[ \t]*halt-height[ \t]*=.*$", "halt-height = 0", head)
if count != 1:
    sys.exit("swap: app.toml must declare halt-height exactly once")
if mode == "reset":
    metadata = path.stat()
    fd, temporary = tempfile.mkstemp(prefix=".app.toml-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as output:
            output.write(updated + separator + tail)
            output.flush()
            os.fchown(output.fileno(), metadata.st_uid, metadata.st_gid)
            os.fchmod(output.fileno(), stat.S_IMODE(metadata.st_mode))
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
elif mode == "verify":
    info_path = home / "data/upgrade-info.json"
    info = json.loads(info_path.read_text())
    if info != {"name": "v6.12.0", "height": halt + 1} or type(info["height"]) is not int:
        sys.exit("swap: node upgrade file differs")
    if stat.S_IMODE(info_path.stat().st_mode) != 0o600:
        sys.exit("swap: node upgrade file mode must be 600")
    if config["halt-height"] != 0:
        sys.exit("swap: halt-height was not reset")
PY
}

check_node() {
  local home=$1
  check_home "$home" verify || return 1
  node_process verify "$home" "$RELEASE_SHA256" || return 1
  python3 - "$home" "$halt" <<'PY'
import json
from pathlib import Path
import sys
import tomllib
import urllib.request
from urllib.parse import urlsplit

home = Path(sys.argv[1])
config = tomllib.loads((home / "config/config.toml").read_text())
app = tomllib.loads((home / "config/app.toml").read_text())


def local_address(host):
    return "localhost" if host in ("0.0.0.0", "::", "") else host


def url(host, port):
    host = local_address(host)
    if ":" in host:
        host = f"[{host}]"
    return f"http://{host}:{port}"


opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
rpc = urlsplit(config["rpc"]["laddr"])
with opener.open(url(rpc.hostname, rpc.port) + "/status", timeout=3) as response:
    status = json.load(response)["sync_info"]
if int(status["latest_block_height"]) < int(sys.argv[2]):
    sys.exit("swap: node has not reached halt height")
evm = app["evm"]
if evm["http_enabled"]:
    request = urllib.request.Request(
        url(evm["http_address"], evm["http_port"]),
        data=json.dumps(
            {"jsonrpc": "2.0", "id": 1, "method": "eth_chainId", "params": []}
        ).encode(),
        headers={"Content-Type": "application/json"},
    )
    with opener.open(request, timeout=3) as response:
        result = json.load(response)
    if "error" in result or int(result["result"], 16) <= 0:
        sys.exit("swap: eth_chainId failed")
PY
}

write_receipt() {
  [[ -n "$receipt" ]] || return 0
  python3 - "$receipt" "${#homes[@]}" <<'PY'
import json
import os
from pathlib import Path
import sys
import tempfile

path, count = Path(sys.argv[1]), int(sys.argv[2])
fd, temporary = tempfile.mkstemp(prefix=".swap-receipt-", dir=path.parent)
try:
    with os.fdopen(fd, "w") as output:
        json.dump({"nodes_checked": count, "nodes_passed": count, "result": "pass"}, output)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, path)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
PY
}

if [[ "$verify" == true ]]; then
  check_binary "$BIN_DIR/paxd"
  for index in "${!homes[@]}"; do
    if [[ "$manager" == systemd ]]; then
      systemctl is-active --quiet "${units[$index]}" || fail "service is not active"
    fi
    check_node "${homes[$index]}" || fail "node verification failed"
  done
  write_receipt
  exit 0
fi

for home in "${validators[@]}"; do
  running=$(node_process list "$home") || fail "process inspection failed"
  [[ -z "$running" ]] || fail "validator home $home still running"
done
check_binary "$BIN_DIR/paxd.v6.12.0"
python3 - "$release_doc" "$RELEASE_SHA256" <<'PY' || exit 1
import json
from pathlib import Path
import sys
from urllib.parse import parse_qs, urlsplit

try:
    document = json.loads(Path(sys.argv[1]).read_text())
    if document["name"] != "v6.12.0":
        sys.exit("swap: release document name differs")
    asset = json.loads(document["info"])["binaries"]["linux/amd64"]
    checksum = parse_qs(urlsplit(asset).query).get("checksum")
    if checksum != ["sha256:" + sys.argv[2]]:
        sys.exit("swap: release document checksum differs")
except (OSError, ValueError, KeyError, TypeError):
    sys.exit("swap: invalid release document")
PY
[[ -f "$root/tools/release/upgrade_info.py" ]] || fail "required interface tools/release/upgrade_info.py is missing"
for home in "${homes[@]}"; do
  check_home "$home" preflight
done
if [[ "$role" == full ]]; then
  for index in "${!homes[@]}"; do
    if [[ "$manager" == systemd ]]; then
      systemctl stop "${units[$index]}"
    else
      node_process stop "${homes[$index]}"
    fi
  done
fi
for home in "${homes[@]}" "${validators[@]}"; do
  running=$(node_process list "$home") || fail "process inspection failed"
  [[ -z "$running" ]] || fail "home $home still running"
done
for home in "${homes[@]}"; do
  python3 "$root/tools/release/upgrade_info.py" node --home "$home" \
    --halt-height "$halt" --release-doc "$release_doc" --sha256 "$RELEASE_SHA256"
  check_home "$home" reset
  check_home "$home" verify
done
[[ ! -d "$BIN_DIR/paxd" ]] || fail "installed paxd is a directory"
(set -o noclobber; : > "$BIN_DIR/paxd.new") || fail "paxd.new already exists"
new_owned=true
cp -p -- "$BIN_DIR/paxd.v6.12.0" "$BIN_DIR/paxd.new"
check_binary "$BIN_DIR/paxd.new"
mv -f -- "$BIN_DIR/paxd.new" "$BIN_DIR/paxd"
new_owned=false
check_binary "$BIN_DIR/paxd"
for index in "${!homes[@]}"; do
  home=${homes[$index]}
  if [[ "$manager" == systemd ]]; then
    systemctl start "${units[$index]}"
  else
    nohup "$BIN_DIR/paxd" start --home "$home" \
      >> "$home/data/release-start.log" 2>&1 < /dev/null 9>&- &
  fi
done
for home in "${homes[@]}"; do
  deadline=$((SECONDS + 60))
  until check_node "$home" > "$home/data/release-verify.log" 2>&1; do
    if ((SECONDS >= deadline)); then
      cat "$home/data/release-verify.log" >&2
      fail "node did not become ready"
    fi
    sleep 1
  done
done
write_receipt
