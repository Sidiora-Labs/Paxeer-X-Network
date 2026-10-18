#!/usr/bin/env bash
set -euo pipefail
umask 077

env_file= mode= receipt= phase= local_only=false
expected_release= expected_height=
manager=systemd
readiness=false
rollback_executed=false

fail() {
  printf 'rollback: %s\n' "$*" >&2
  exit 1
}
need_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || fail "$1 needs a value"; }
finish() {
  local rc=$? result=fail tmp
  trap - EXIT
  if [ "$rc" -eq 0 ]; then result=pass; fi
  if [ -n "$receipt" ]; then
    tmp=$(mktemp "${receipt}.XXXXXX") || exit 1
    printf '{"readiness":%s,"rollback_executed":%s,"result":"%s"}\n' \
      "$readiness" "$rollback_executed" "$result" >"$tmp"
    mv -fT -- "$tmp" "$receipt" || exit 1
  fi
  exit "$rc"
}
trap finish EXIT

while [ "$#" -gt 0 ]; do
  case $1 in
  --env-file)
    need_value "$@"
    env_file=$2
    shift 2
    ;;
  --receipt)
    need_value "$@"
    receipt=$2
    shift 2
    ;;
  --service-manager)
    need_value "$@"
    manager=$2
    shift 2
    ;;
  --local)
    local_only=true
    shift
    ;;
  --phase)
    need_value "$@"
    phase=$2
    shift 2
    ;;
  --expected-release-id)
    need_value "$@"
    expected_release=$2
    shift 2
    ;;
  --expected-halt-height)
    need_value "$@"
    expected_height=$2
    shift 2
    ;;
  --execute | --readiness | --verify)
    [ -z "$mode" ] || fail "choose exactly one mode"
    mode=${1#--}
    shift
    ;;
  *) fail "unknown argument $1" ;;
  esac
done
[ -n "$mode" ] || fail "choose --execute, --readiness or --verify"
case $manager in systemd | foreground) ;; *) fail "invalid service manager" ;; esac
[ -r "$env_file" ] || fail "--env-file is not readable"

declare -A input=()
while IFS= read -r line || [ -n "$line" ]; do
  line=${line#"${line%%[![:space:]]*}"}
  case $line in '' | \#*) continue ;; esac
  line=${line#export }
  [[ $line =~ ^([A-Z_][A-Z0-9_]*)=(.*)$ ]] || fail "env file is not KEY=VALUE"
  key=${BASH_REMATCH[1]}
  value=${BASH_REMATCH[2]}
  value=${value%"${value##*[![:space:]]}"}
  if [[ $value =~ ^\"(.*)\"$ ]] || [[ $value =~ ^\'(.*)\'$ ]]; then value=${BASH_REMATCH[1]}; fi
  [ -z "${input[$key]+present}" ] || fail "duplicate input $key"
  input[$key]=$value
done <"$env_file"
require() { [ -n "${input[$1]:-}" ] || fail "$1 is empty"; }

hosts=()
read -r -a validator_hosts <<<"${input[VALIDATOR_HOSTS]:-}"
read -r -a fullnode_hosts <<<"${input[FULLNODE_HOSTS]:-}"
if [ -n "${input[ARCHIVE_HOST]:-}" ]; then fullnode_hosts+=("${input[ARCHIVE_HOST]}"); fi
declare -A seen_hosts=()
for host in "${validator_hosts[@]}" "${fullnode_hosts[@]}"; do
  [[ $host =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.@:-]*$ ]] || fail "invalid host"
  if [ -z "${seen_hosts[$host]+present}" ]; then
    hosts+=("$host")
    seen_hosts[$host]=1
  fi
done

remote_phase() {
  local host=$1 step=$2 command
  printf -v command 'bash -s -- --env-file %q --local --%s --service-manager systemd' "$env_file" "$mode"
  printf -v command '%s --expected-release-id %q --expected-halt-height %q' \
    "$command" "${input[RELEASE_ID]}" "${input[HALT_HEIGHT]}"
  if [ -n "$step" ]; then printf -v command '%s --phase %q' "$command" "$step"; fi
  ssh -o BatchMode=yes -o ConnectTimeout=10 -- "$host" "$command" <"${BASH_SOURCE[0]}"
}

if ! "$local_only" && [ "${#hosts[@]}" -gt 0 ]; then
  require RELEASE_ID
  require HALT_HEIGHT
  [ "$manager" = systemd ] || fail "foreground requires --local"
  [ -z "$phase" ] || fail "--phase requires --local"
  if [ "$mode" = execute ]; then
    for step in prepare stop restore start; do
      rc=0
      for host in "${hosts[@]}"; do remote_phase "$host" "$step" || rc=1; done
      [ "$rc" -eq 0 ] || fail "fleet $step failed; no later phase started"
      if [ "$step" = prepare ]; then readiness=true; fi
    done
    rollback_executed=true
  else
    rc=0
    for host in "${hosts[@]}"; do remote_phase "$host" '' || rc=1; done
    [ "$rc" -eq 0 ] || fail "fleet $mode failed"
    readiness=true
    if [ "$mode" = verify ]; then rollback_executed=true; fi
  fi
  exit 0
fi

for key in RELEASE_ID BIN_DIR SNAPSHOT_DIR HALT_HEIGHT; do require "$key"; done
release_id=${input[RELEASE_ID]}
bin_dir=${input[BIN_DIR]}
snapshot_dir=${input[SNAPSHOT_DIR]}
halt_height=${input[HALT_HEIGHT]}
[ -z "$expected_release" ] || [ "$expected_release" = "$release_id" ] || fail "fleet RELEASE_ID differs"
[ -z "$expected_height" ] || [ "$expected_height" = "$halt_height" ] || fail "fleet HALT_HEIGHT differs"
[[ $release_id =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]] || fail "invalid RELEASE_ID"
[[ $halt_height =~ ^[1-9][0-9]{0,17}$ ]] || fail "invalid HALT_HEIGHT"
[[ $bin_dir = /* && $snapshot_dir = /* ]] || fail "directories must be absolute"
bin_dir=$(realpath -e -- "$bin_dir")
snapshot_dir=$(realpath -e -- "$snapshot_dir")
exec 9>"$bin_dir/.rollback.lock"
flock -n 9 || fail "another rollback is running"
previous=$bin_dir/paxd.pre-$release_id
homes=() units=() validator_homes=() fullnode_homes=() validator_units=() fullnode_units=()
read -r -a validator_homes <<<"${input[VALIDATOR_HOMES]:-}"
read -r -a fullnode_homes <<<"${input[FULLNODE_HOMES]:-}"
read -r -a validator_units <<<"${input[VALIDATOR_UNITS]:-}"
read -r -a fullnode_units <<<"${input[FULLNODE_UNITS]:-}"
[ "${#validator_homes[@]}" -eq "${#validator_units[@]}" ] || fail "validator homes and units differ"
[ "${#fullnode_homes[@]}" -eq "${#fullnode_units[@]}" ] || fail "full-node homes and units differ"
homes=("${validator_homes[@]}" "${fullnode_homes[@]}")
units=("${validator_units[@]}" "${fullnode_units[@]}")
[ "${#homes[@]}" -gt 0 ] || fail "VALIDATOR_HOMES and FULLNODE_HOMES are empty"
declare -A seen_homes=() seen_units=() seen_archives=()
for i in "${!homes[@]}"; do
  home=${homes[$i]}
  [[ $home = /* && $home != / && $home != *'/../'* && $home != */data.v6.12.0-* ]] || fail "invalid home"
  [ "$(realpath -e -- "$home")" = "$home" ] || fail "home must be canonical: $home"
  name=${home##*/}
  unit=${units[$i]}
  [[ $unit =~ ^[a-zA-Z0-9][a-zA-Z0-9_.@:-]*$ ]] || fail "invalid unit"
  [ -z "${seen_homes[$home]+present}${seen_units[$unit]+present}${seen_archives[$name]+present}" ] || fail "duplicate home, unit or archive name"
  for other in "${!seen_homes[@]}"; do
    [[ $home != "$other/"* && $other != "$home/"* ]] || fail "nested homes"
  done
  seen_homes[$home]=1
  seen_units[$unit]=1
  seen_archives[$name]=1
done

sha() { sha256sum -- "$1" | cut -d' ' -f1; }
archive() { printf '%s/%s-pre-v6.12.0.tar.zst' "$snapshot_dir" "${1##*/}"; }
record_dir() { printf '%s/.rollback-%s' "$1" "$release_id"; }
set_aside() { printf '%s/data.v6.12.0-%s' "$1" "$release_id"; }

check_archive() {
  local file=$1 role=$2
  tar --zstd -tf "$file" >/dev/null || fail "invalid snapshot"
  python3 - "$file" "$halt_height" "$role" <<'PY'
import json
import pathlib
import subprocess
import sys
import tarfile


def halt_proposal(state, halt):
    if (str(state.get("height")) != str(halt + 1) or state.get("round") != 0 or state.get("step") != 1
            or not isinstance(state.get("signature"), str) or not state["signature"]
            or not isinstance(state.get("signbytes"), str)):
        return False
    try:
        signbytes = bytes.fromhex(state["signbytes"])
    except ValueError:
        return False
    length = shift = offset = 0
    while offset < len(signbytes) and offset < 10:
        byte = signbytes[offset]
        offset += 1
        length |= (byte & 0x7F) << shift
        shift += 7
        if not byte & 0x80:
            break
    else:
        return False
    body = signbytes[offset:]
    return length == len(body) and body[:2] == b"\x08\x20"


with subprocess.Popen(["zstd", "-dc", "--", sys.argv[1]], stdout=subprocess.PIPE) as process:
    try:
        seen = set()
        state = None
        store = False
        with tarfile.open(fileobj=process.stdout, mode="r|") as archive:
            for member in archive:
                path = pathlib.PurePosixPath(member.name)
                name = str(path)
                if (path.is_absolute() or ".." in path.parts or not path.parts
                        or path.parts[0] != "data" or name in seen
                        or not (member.isfile() or member.isdir())):
                    raise ValueError("unsafe snapshot member")
                seen.add(name)
                if name.startswith("data/application.db/") or name == "data/application.db":
                    store = True
                if name == "data/priv_validator_state.json" and member.isfile():
                    if member.size > 65536:
                        raise ValueError("invalid signing state size")
                    state = json.load(archive.extractfile(member))
        if not store or state is None:
            raise ValueError("snapshot lacks application store or signing state")
        height = state.get("height")
        if sys.argv[3] == "validator":
            if str(height) != sys.argv[2] and not halt_proposal(state, int(sys.argv[2])):
                raise ValueError("snapshot signing height differs from HALT_HEIGHT")
        elif not str(height).isdigit() or not 0 <= int(height) <= int(sys.argv[2]):
            raise ValueError("snapshot signing state is after HALT_HEIGHT")
        process.stdout.close()
        if process.wait() != 0:
            raise ValueError("snapshot decompression failed")
    except (ValueError, KeyError, tarfile.TarError, OSError) as error:
        process.stdout.close()
        process.wait()
        sys.exit("rollback: " + str(error))
PY
}

matching_pids() {
  local home=$1 candidates rc=0
  candidates=$(pgrep -a -x paxd) || rc=$?
  [ "$rc" -le 1 ] || fail "process enumeration failed"
  python3 - "$home" "$candidates" <<'PY'
import pathlib
import sys

for line in sys.argv[2].splitlines():
    pid = line.split()[0]
    try:
        args = pathlib.Path("/proc", pid, "cmdline").read_bytes().split(b"\0")
    except FileNotFoundError:
        continue
    home = sys.argv[1].encode()
    if any(arg == b"--home=" + home or (arg == b"--home" and i + 1 < len(args)
           and args[i + 1] == home) for i, arg in enumerate(args)):
        print(pid)
PY
}

check_stopped() {
  local home i state
  for i in "${!homes[@]}"; do
    home=${homes[$i]}
    [ -z "$(matching_pids "$home")" ] || fail "node still running for $home"
    [ -z "$(matching_pids "$(set_aside "$home")")" ] || fail "set-aside data is running"
    if [ "$manager" = systemd ]; then
      state=$(systemctl show --property=ActiveState --value "${units[$i]}")
      case $state in inactive | failed) ;; *) fail "unit is not stopped" ;; esac
    fi
  done
}

check_material() {
  local i home file role
  [ -f "$previous" ] && [ -x "$previous" ] && [ ! -L "$previous" ] || fail "no previous binary"
  previous_sha=$(sha "$previous")
  for i in "${!homes[@]}"; do
    home=${homes[$i]}
    file=$(archive "$home")
    role=full
    if [ "$i" -lt "${#validator_homes[@]}" ]; then role=validator; fi
    [ -f "$file" ] && [ -r "$file" ] || fail "no snapshot for $home"
    check_archive "$file" "$role"
  done
  readiness=true
}

check_binary() {
  local binary=$1 libraries
  [ "$(sha "$binary")" = "$previous_sha" ] || fail "previous binary checksum mismatch"
  libraries=$(ldd "$binary" 2>&1) || fail "previous binary dependency check failed"
  [[ $libraries != *'not found'* ]] || fail "previous binary has unresolved libraries"
}

prepare() {
  local home path
  check_material
  check_binary "$previous"
  for home in "${homes[@]}"; do
    [ -d "$home/data" ] && [ ! -L "$home/data" ] || fail "invalid data directory for $home"
    for path in "$(set_aside "$home")" "$(record_dir "$home")"; do
      [ ! -e "$path" ] && [ ! -L "$path" ] || fail "rollback destination already exists for $home"
    done
    [ -f "$home/config/app.toml" ] && [ ! -L "$home/config/app.toml" ] || fail "invalid app config"
    [ "$(grep -cE '^[[:space:]]*halt-height[[:space:]]*=' "$home/config/app.toml")" -eq 1 ] || fail "expected one halt-height"
    python3 - "$home/config/app.toml" <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as stream:
    config = tomllib.load(stream)
height = config.get("halt-height")
if type(height) is not int or height < 0:
    sys.exit("rollback: invalid halt-height")
PY
  done
  [ ! -e "$bin_dir/paxd.new" ] && [ ! -L "$bin_dir/paxd.new" ] || fail "paxd.new already exists"
}

stop_nodes() {
  local unit home pid attempt rc=0
  if [ "$manager" = systemd ]; then
    read -r -a sidecars <<<"${input[SIDECAR_UNITS]:-}"
    for unit in "${sidecars[@]}" "${units[@]}"; do systemctl stop -- "$unit" || rc=1; done
    for unit in "${units[@]}"; do
      state=$(systemctl show --property=ActiveState --value "$unit") || rc=1
      case $state in inactive | failed) ;; *) rc=1 ;; esac
    done
  else
    [ -z "${input[SIDECAR_UNITS]:-}" ] || fail "foreground cannot manage sidecar units"
    for home in "${homes[@]}"; do
      for pid in $(matching_pids "$home"); do kill -TERM "$pid" || rc=1; done
    done
  fi
  [ "$rc" -eq 0 ] || fail "could not stop every unit"
  for ((attempt = 0; attempt < 120; attempt++)); do
    rc=0
    for home in "${homes[@]}"; do [ -z "$(matching_pids "$home")" ] || rc=1; done
    if [ "$rc" -eq 0 ]; then
      check_stopped
      return
    fi
    sleep 1
  done
  fail "nodes did not stop"
}

restore() {
  local home record file
  prepare
  check_stopped
  for home in "${homes[@]}"; do
    file=$(archive "$home")
    record=$(record_dir "$home")
    mkdir -- "$record"
    mv -T -- "$home/data" "$(set_aside "$home")"
    tar --zstd -xf "$file" -C "$home" --no-same-owner
    tar --zstd -df "$file" -C "$home"
    cp -p -- "$home/data/priv_validator_state.json" "$record/priv_validator_state.json"
    rm -f -- "$home/data/upgrade-info.json"
    sed -i -E 's/^[[:space:]]*halt-height[[:space:]]*=.*/halt-height = 0/' "$home/config/app.toml"
    printf '%s\n%s\n%s\n' "$previous_sha" "$(sha "$file")" "$halt_height" >"$record/material"
  done
  (
    set -o noclobber
    : >"$bin_dir/paxd.new"
  )
  cp -p -- "$previous" "$bin_dir/paxd.new"
  check_binary "$bin_dir/paxd.new"
  mv -fT -- "$bin_dir/paxd.new" "$bin_dir/paxd"
  check_binary "$bin_dir/paxd"
  for home in "${homes[@]}"; do : >"$(record_dir "$home")/restored"; done
}

check_restored() {
  local home record want
  check_material
  check_binary "$bin_dir/paxd"
  for home in "${homes[@]}"; do
    record=$(record_dir "$home")
    [ -d "$(set_aside "$home")" ] && [ ! -L "$(set_aside "$home")" ] || fail "no retained data for $home"
    [ -d "$home/data" ] && [ ! -L "$home/data" ] || fail "invalid restored data"
    [ -f "$record/restored" ] && [ ! -L "$record" ] || fail "no completed restore for $home"
    want=$(printf '%s\n%s\n%s' "$previous_sha" "$(sha "$(archive "$home")")" "$halt_height")
    [ "$(cat "$record/material")" = "$want" ] || fail "restored material changed"
    [ ! -e "$home/data/upgrade-info.json" ] && [ ! -L "$home/data/upgrade-info.json" ] || fail "upgrade file remains"
    grep -qx 'halt-height = 0' "$home/config/app.toml" || fail "halt-height is not zero"
    [ -z "$(matching_pids "$(set_aside "$home")")" ] || fail "set-aside data is running"
  done
}

start_nodes() {
  local i home
  check_stopped
  check_restored
  for i in "${!homes[@]}"; do
    home=${homes[$i]}
    if [ "$manager" = systemd ]; then
      systemctl start -- "${units[$i]}"
      systemctl is-active --quiet "${units[$i]}"
    else
      nohup "$bin_dir/paxd" start --home "$home" >"$(record_dir "$home")/node.log" 2>&1 </dev/null 9>&- &
    fi
    : >"$(record_dir "$home")/started"
  done
  rollback_executed=true
}

verify_nodes() {
  local home pids pid before after attempt endpoint
  check_restored
  for home in "${homes[@]}"; do
    [ -f "$(record_dir "$home")/started" ] || fail "rollback did not restart $home"
    endpoint=$(
      python3 - "$home/config/config.toml" <<'PY'
import sys
import tomllib
from urllib.parse import urlsplit

with open(sys.argv[1], "rb") as stream:
    endpoint = tomllib.load(stream)["rpc"]["laddr"]
parts = urlsplit(endpoint)
if parts.scheme not in ("tcp", "http", "https") or not parts.hostname or not parts.port:
    sys.exit("rollback: invalid consensus RPC address")
host = parts.hostname
if host in ("0.0.0.0", "::"):
    host = "localhost"
if ":" in host:
    host = "[" + host + "]"
print(("https" if parts.scheme == "https" else "http") + "://" + host + ":" + str(parts.port))
PY
    )
    before=
    for ((attempt = 0; attempt < 120; attempt++)); do
      pids=$(matching_pids "$home")
      if [[ $pids =~ ^[0-9]+$ ]]; then
        pid=$pids
        [ "$(sha "/proc/$pid/exe")" = "$previous_sha" ] || fail "running binary checksum mismatch"
        if after=$(curl --fail --silent --show-error --max-time 3 "$endpoint/status" 2>/dev/null |
          jq -er '.result.sync_info | select(.catching_up == false) | .latest_block_height | select(test("^[0-9]{1,18}$"))'); then
          if [ -n "$before" ] && [ "$after" -gt "$before" ] && [ "$after" -gt "$halt_height" ]; then break; fi
          if [ -z "$before" ]; then before=$after; fi
        fi
      elif [ -n "$pids" ]; then
        fail "multiple processes for $home"
      fi
      sleep 1
    done
    [ "$attempt" -lt 120 ] || fail "chain height did not rise past HALT_HEIGHT for $home"
    [ ! -e "$home/data/upgrade-info.json" ] || fail "upgrade file reappeared"
  done
  rollback_executed=true
}

case $mode in
readiness)
  [ -z "$phase" ] || fail "phase requires --execute"
  check_material
  ;;
verify)
  [ -z "$phase" ] || fail "phase requires --execute"
  verify_nodes
  ;;
execute)
  if [ -n "$phase" ]; then
    "$local_only" || fail "phase requires --local"
    case $phase in
    prepare) prepare ;;
    stop) stop_nodes ;;
    restore) restore ;;
    start) start_nodes ;;
    *) fail "invalid phase" ;;
    esac
  else
    prepare
    stop_nodes
    restore
    start_nodes
  fi
  ;;
esac
