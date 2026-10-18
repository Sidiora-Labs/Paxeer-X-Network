#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
work_base=${WORK:-$root/.logs}
mkdir -p "$work_base"
work=$(mktemp -d "$work_base/swap.XXXXXX")
passed=0
failed=0

cleanup() {
  python3 - "$work" <<'PY'
import os
from pathlib import Path
import select
import signal
import subprocess
import sys

root = Path(sys.argv[1]).resolve()
result = subprocess.run(["pgrep", "-a", "-x", "paxd"], capture_output=True, text=True)
if result.returncode not in (0, 1):
    sys.exit("swap tests: unable to inspect child processes")
for line in result.stdout.splitlines():
    pid = int(line.split(maxsplit=1)[0])
    try:
        fd = os.pidfd_open(pid)
    except ProcessLookupError:
        continue
    try:
        args = Path(f"/proc/{pid}/cmdline").read_bytes().split(b"\0")
        homes = []
        for index, arg in enumerate(args):
            if arg == b"--home" and index + 1 < len(args):
                homes.append(Path(os.fsdecode(args[index + 1])).resolve())
            elif arg.startswith(b"--home="):
                homes.append(Path(os.fsdecode(arg.partition(b"=")[2])).resolve())
        if any(root in home.parents for home in homes):
            signal.pidfd_send_signal(fd, signal.SIGTERM)
            poller = select.poll()
            poller.register(fd, select.POLLIN)
            if not poller.poll(60000):
                sys.exit("swap tests: child did not stop cleanly")
    except (FileNotFoundError, ProcessLookupError):
        pass
    finally:
        os.close(fd)
PY
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

missing=0
for dependency in tools/release/upgrade_info.py tools/release/stage.sh; do
  if [[ ! -f "$dependency" ]]; then
    printf 'FAIL prerequisite: required interface %s is missing\n' "$dependency" >&2
    missing=$((missing + 1))
  fi
done
if ((missing)); then
  printf 'swap: 0 passed, %s failed prerequisites; 6 scenarios not run\n' "$missing"
  exit 1
fi

candidate=${NEW_BIN:-}
if [[ -z "$candidate" ]]; then
  candidate=$work/candidate/paxd
  mkdir -p "$(dirname "$candidate")"
  package=$(go list -m)/sdk/version
  go build -p 2 -trimpath -buildvcs=false -tags 'netgo ledger' \
    -ldflags "-checklinkname=0 -X $package.Version=v6.12.0" \
    -o "$candidate" ./daemon/paxd
fi
candidate=$(realpath "$candidate")
[[ -x "$candidate" ]]
[[ $("$candidate" version) == v6.12.0 ]]

hash() {
  sha256sum "$1" | cut -d ' ' -f 1
}

previous_commit=ab0ea61c8
previous=${OLD_BIN:-$root/build/paxd.previous}
if [[ -z "${OLD_BIN:-}" && ! -e "$previous" ]]; then
  previous_src=$root/build/paxd.previous.src
  rm -rf -- "$previous_src"
  mkdir -p "$previous_src"
  git -C "$root" archive "$previous_commit" | tar -x -C "$previous_src"
  (
    cd "$previous_src"
    package=$(go list -m)/sdk/version
    go build -p 2 -trimpath -buildvcs=false -tags 'netgo ledger' \
      -ldflags "-checklinkname=0 -X $package.Version=v6.10" \
      -o "$previous" ./daemon/paxd
  )
fi
refuse_previous() {
  printf 'FAIL prerequisite: %s\n' "$1" >&2
  printf 'swap: 0 passed, 1 failed prerequisites; 6 scenarios not run\n'
  exit 1
}
[[ -f "$previous" && -x "$previous" ]] || refuse_previous "OLD_BIN must name the real previous paxd binary: $previous"
previous=$(realpath "$previous")
previous_version=$("$previous" version) || refuse_previous "OLD_BIN does not report a version: $previous"
[[ "$previous_version" != v6.12.0 ]] || refuse_previous "OLD_BIN reports the candidate version v6.12.0: $previous"
[[ $(hash "$previous") != "$(hash "$candidate")" ]] || refuse_previous "OLD_BIN and NEW_BIN must be different binaries"

sidecar=${SIDECAR_BIN:-$root/dist/paxeer-network/v6.12.0/price-sidecar-v6.12.0-linux-amd64}
if [[ -z "${SIDECAR_BIN:-}" && ! -f "$sidecar" ]]; then
  cargo build --locked --release --manifest-path platform/Cargo.toml -p layerx-oracle-feeder --bin price-sidecar
  platform_target=$(cargo metadata --locked --format-version 1 --no-deps --manifest-path platform/Cargo.toml | jq -er '.target_directory')
  sidecar=$platform_target/release/price-sidecar
fi
[[ -f "$sidecar" && -x "$sidecar" ]] || refuse_previous "price-sidecar binary is missing: $sidecar"
sidecar=$(realpath "$sidecar")

write_release() {
  printf '%s  paxd-v6.12.0-linux-amd64\n' "$RELEASE_SHA256" > "$case_dir/SHA256SUMS"
  python3 "$root/tools/release/upgrade_info.py" release --sums "$case_dir/SHA256SUMS" \
    --tag paxeer-network/v6.12.0 --out "$case_dir/upgrade-info.json"
}

setup_case() {
  case_dir=$work/$1
  mkdir -p "$case_dir/bin"
  export BIN_DIR=$case_dir/bin
  cp -p "$previous" "$BIN_DIR/paxd"
  cp -p "$candidate" "$BIN_DIR/paxd.v6.12.0"
  export RELEASE_SHA256
  RELEASE_SHA256=$(hash "$candidate")
  export VALIDATOR_HOMES= VALIDATOR_UNITS= FULLNODE_HOMES= FULLNODE_UNITS=
  export HALT_HEIGHT=20
  write_release
}

new_home() {
  local name=$1 halt=$2 address denom
  node_home=$case_dir/$name
  "$BIN_DIR/paxd" init swap --chain-id swap_9000-1 --mode validator --home "$node_home" \
    > "$case_dir/$name-init.log" 2>&1
  "$BIN_DIR/paxd" keys add operator --keyring-backend test --home "$node_home" \
    > "$case_dir/$name-key.log" 2>&1
  address=$("$BIN_DIR/paxd" keys show operator -a --keyring-backend test --home "$node_home")
  denom=$(python3 - "$node_home/config/genesis.json" <<'PY'
import json
import sys

with open(sys.argv[1]) as stream:
    print(json.load(stream)["app_state"]["staking"]["params"]["bond_denom"])
PY
)
  "$BIN_DIR/paxd" add-genesis-account "$address" "100000000000000000000$denom" --home "$node_home" \
    > "$case_dir/$name-account.log" 2>&1
  "$BIN_DIR/paxd" gentx operator "1000000000000000000$denom" --chain-id swap_9000-1 \
    --keyring-backend test --home "$node_home" > "$case_dir/$name-gentx.log" 2>&1
  python3 - "$node_home" <<'PY'
import json
from pathlib import Path
import sys

home = Path(sys.argv[1])
genesis_path = home / "config/genesis.json"
genesis = json.loads(genesis_path.read_text())
public_key = json.loads((home / "config/priv_validator_key.json").read_text())["pub_key"]
genesis["validators"] = [{"power": "1000000000000", "pub_key": public_key}]
genesis["app_state"]["staking"]["params"]["max_voting_power_ratio"] = "1.000000000000000000"
genesis_path.write_text(json.dumps(genesis))
PY
  "$BIN_DIR/paxd" collect-gentxs --home "$node_home" > "$case_dir/$name-collect.log" 2>&1
  python3 -I "$root/tools/release/tests/seed_halt_export.py" "$root/node/testdata/v6120-halt-export.json" \
    "$node_home/config/genesis.json"
  "$BIN_DIR/paxd" validate-genesis --home "$node_home" > "$case_dir/$name-validate.log" 2>&1
  python3 - "$node_home" "$halt" "$denom" <<'PY'
import json
from pathlib import Path
import re
import socket
import sys
import tomllib

home, halt, denom = Path(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
listeners = [socket.socket() for _ in range(4)]
loopback = socket.gethostbyname("localhost")
for listener in listeners:
    listener.bind((loopback, 0))
rpc, peer, http, ws = [listener.getsockname()[1] for listener in listeners]


def edit(path, changes):
    section = ""
    seen = set()
    lines = []
    for line in path.read_text().splitlines():
        heading = re.fullmatch(r"\s*\[([^]]+)\]\s*", line)
        if heading:
            section = heading[1]
        key = re.match(r"\s*([\w-]+)\s*=", line)
        pair = (section, key[1]) if key else None
        if pair in changes:
            line = f"{key[1]} = {changes[pair]}"
            seen.add(pair)
        lines.append(line)
    assert seen == set(changes), set(changes) - seen
    text = "\n".join(lines) + "\n"
    tomllib.loads(text)
    path.write_text(text)


edit(home / "config/config.toml", {
    ("rpc", "laddr"): json.dumps(f"tcp://{loopback}:{rpc}"),
    ("rpc", "pprof-laddr"): '""',
    ("p2p", "laddr"): json.dumps(f"tcp://{loopback}:{peer}"),
    ("p2p", "persistent-peers"): '""',
    ("p2p", "bootstrap-peers"): '""',
    ("consensus", "create-empty-blocks"): "true",
    ("consensus", "create-empty-blocks-interval"): '"10ms"',
    ("consensus", "unsafe-commit-timeout-override"): '"10ms"',
})
edit(home / "config/app.toml", {
    ("", "halt-height"): str(halt),
    ("", "minimum-gas-prices"): json.dumps("0" + denom),
    ("api", "enable"): "false",
    ("grpc", "enable"): "false",
    ("grpc-web", "enable"): "false",
    ("evm", "http_enabled"): "true",
    ("evm", "http_address"): json.dumps(loopback),
    ("evm", "http_port"): str(http),
    ("evm", "ws_enabled"): "true",
    ("evm", "ws_port"): str(ws),
})
for listener in listeners:
    listener.close()
PY
}

swap() {
  bash "$root/tools/release/swap.sh" --service-manager foreground \
    --release-doc "$case_dir/upgrade-info.json" "$@"
}

expect_refusal() {
  local expected=$1 status=0
  shift
  "$@" > "$case_dir/refusal.log" 2>&1 || status=$?
  [[ "$status" == 1 ]]
  grep -Fx -- "$expected" "$case_dir/refusal.log"
}

wait_height() {
  python3 - "$node_home" "$1" <<'PY'
import json
from pathlib import Path
import sys
import time
import tomllib
import urllib.error
import urllib.request

home, height = Path(sys.argv[1]), int(sys.argv[2])
config = tomllib.loads((home / "config/config.toml").read_text())
url = config["rpc"]["laddr"].replace("tcp://", "http://") + "/status"
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
deadline = time.monotonic() + 180
while time.monotonic() < deadline:
    try:
        with opener.open(url, timeout=2) as response:
            current = int(json.load(response)["sync_info"]["latest_block_height"])
        if current >= height:
            break
    except (OSError, ValueError, KeyError, urllib.error.URLError):
        pass
    time.sleep(0.2)
else:
    sys.exit(f"chain did not reach height {height}; inspect {home}/data/release-start.log")
PY
}

start_until_halt() {
  local halt=$1 status=0
  "$BIN_DIR/paxd" start --home "$node_home" --halt-height "$halt" \
    > "$node_home/data/halt.log" 2>&1 &
  local pid=$!
  python3 - "$pid" <<'PY'
import os
import select
import sys

try:
    fd = os.pidfd_open(int(sys.argv[1]))
except ProcessLookupError:
    sys.exit(0)
try:
    poller = select.poll()
    poller.register(fd, select.POLLIN)
    if not poller.poll(300000):
        sys.exit("chain did not halt within the local-chain bound")
finally:
    os.close(fd)
PY
  wait "$pid" || status=$?
  [[ "$status" == 0 ]]
  python3 - "$node_home/data/halt.log" "$halt" "$node_home/data/priv_validator_state.json" <<'PY'
import json
from pathlib import Path
import re
import sys

lines = Path(sys.argv[1]).read_text()
lines = re.sub(r"\x1b\[[0-9;]*m", "", lines)
assert any(
    "halting node per configuration" in line
    and re.search(rf"\bheight={sys.argv[2]}\b", line)
    for line in lines.splitlines()
), lines[-4000:]
state = json.loads(Path(sys.argv[3]).read_text())
halt = int(sys.argv[2])
height = int(state["height"])
if height != halt:
    assert height == halt + 1, state
    assert state.get("round") == 0 and state.get("step") == 1, state
    assert isinstance(state.get("signature"), str) and state["signature"], state
    signbytes = bytes.fromhex(state.get("signbytes") or "")
    assert signbytes, state
    length, shift, offset = 0, 0, 0
    while True:
        assert offset < len(signbytes), state
        byte = signbytes[offset]
        offset += 1
        length |= (byte & 0x7F) << shift
        shift += 7
        if not byte & 0x80:
            break
    assert signbytes[offset:offset + 2] == b"\x08\x20", state
PY
}

assert_upgrade() {
  python3 - "$node_home" "$1" <<'PY'
import json
from pathlib import Path
import stat
import sys
import tomllib

home, height = Path(sys.argv[1]), int(sys.argv[2])
path = home / "data/upgrade-info.json"
assert json.loads(path.read_text()) == {"name": "v6.12.0", "height": height}
assert stat.S_IMODE(path.stat().st_mode) == 0o600
assert tomllib.loads((home / "config/app.toml").read_text())["halt-height"] == 0
PY
}

swap_is_atomic_and_checked() {
  setup_case atomic
  new_home node 20
  FULLNODE_HOMES=$node_home
  local before inode config_hash
  before=$(hash "$BIN_DIR/paxd")
  inode=$(stat -c '%i' "$BIN_DIR/paxd")
  config_hash=$(hash "$node_home/config/app.toml")
  if [[ ${RELEASE_SHA256:0:1} == 0 ]]; then
    RELEASE_SHA256=1${RELEASE_SHA256:1}
  else
    RELEASE_SHA256=0${RELEASE_SHA256:1}
  fi
  expect_refusal 'swap: paxd sha256 mismatch' swap --role full
  [[ $(hash "$BIN_DIR/paxd") == "$before" ]]
  [[ $(stat -c '%i' "$BIN_DIR/paxd") == "$inode" ]]
  [[ $(hash "$node_home/config/app.toml") == "$config_hash" ]]
  [[ ! -e "$node_home/data/upgrade-info.json" && ! -e "$BIN_DIR/paxd.new" ]]
  RELEASE_SHA256=$(hash "$candidate")
  start_until_halt 20
  exec 8< "$BIN_DIR/paxd"
  swap --role full
  [[ $(stat -c '%i' "$BIN_DIR/paxd") != "$inode" ]]
  [[ $(stat -Lc '%i' /dev/fd/8) == "$inode" ]]
  [[ $(hash "$BIN_DIR/paxd") == "$RELEASE_SHA256" ]]
  [[ $(sha256sum <&8 | cut -d ' ' -f 1) == "$before" ]]
  exec 8<&-
  [[ $(stat -c '%a:%Y' "$BIN_DIR/paxd") == "$(stat -c '%a:%Y' "$BIN_DIR/paxd.v6.12.0")" ]]
  [[ ! -e "$BIN_DIR/paxd.new" ]]
}

shared_binary_waits_for_halt() {
  setup_case shared
  new_home stopped 20
  local stopped=$node_home
  start_until_halt 20
  new_home active 0
  local active=$node_home before inode
  before=$(hash "$BIN_DIR/paxd")
  inode=$(stat -c '%i' "$BIN_DIR/paxd")
  "$BIN_DIR/paxd" start --home="$active" > "$active/data/running.log" 2>&1 &
  local pid=$!
  wait_height 2
  VALIDATOR_HOMES="$active $stopped"
  FULLNODE_HOMES=$stopped
  expect_refusal "swap: validator home $active still running" swap --role validator
  expect_refusal "swap: validator home $active still running" swap --role full
  [[ $(hash "$BIN_DIR/paxd") == "$before" ]]
  [[ $(stat -c '%i' "$BIN_DIR/paxd") == "$inode" ]]
  [[ ! -e "$stopped/data/upgrade-info.json" ]]
  kill -TERM "$pid"
  wait "$pid"
}

upgrade_file_and_halt_reset() {
  setup_case upgrade_file
  new_home node 1000
  VALIDATOR_HOMES=$node_home
  HALT_HEIGHT=1000
  local before config_hash
  before=$(hash "$BIN_DIR/paxd")
  config_hash=$(hash "$node_home/config/app.toml")
  python3 - "$case_dir/upgrade-info.json" <<'PY'
import json
from pathlib import Path
import sys

path = Path(sys.argv[1])
document = json.loads(path.read_text())
info = json.loads(document["info"])
asset = info["binaries"]["linux/amd64"]
info["binaries"]["linux/amd64"] = asset[:-1] + ("0" if asset[-1] != "0" else "1")
document["info"] = json.dumps(info)
path.write_text(json.dumps(document))
PY
  expect_refusal 'swap: release document checksum differs' swap --role validator --halt-height 1000
  [[ $(hash "$BIN_DIR/paxd") == "$before" ]]
  [[ $(hash "$node_home/config/app.toml") == "$config_hash" ]]
  [[ ! -e "$node_home/data/upgrade-info.json" ]]
  write_release
  start_until_halt 1000
  swap --role validator --halt-height 1000
  assert_upgrade 1001
  swap --role validator --halt-height 1000 --verify
}

receipts_use_key_set() {
  setup_case receipts
  new_home node 20
  FULLNODE_HOMES=$node_home
  start_until_halt 20
  export RELEASE_ID=receipt-check
  export SIDECAR_SHA256
  export SIDECAR_UNITS= AUTOMATION_UNITS= OLD_FEEDER_UNITS=
  cp -p "$sidecar" "$case_dir/price-sidecar-v6.12.0-linux-amd64"
  SIDECAR_SHA256=$(hash "$case_dir/price-sidecar-v6.12.0-linux-amd64")
  cp -p "$candidate" "$case_dir/paxd-v6.12.0-linux-amd64"
  python3 - "$case_dir" "$node_home" "$RELEASE_SHA256" "$SIDECAR_SHA256" <<'PY'
from pathlib import Path
import sys
import tomllib

directory, home = Path(sys.argv[1]), Path(sys.argv[2])
config = tomllib.loads((home / "config/config.toml").read_text())
app = tomllib.loads((home / "config/app.toml").read_text())
rpc = config["rpc"]["laddr"].replace("tcp://", "http://")
evm = f'http://{app["evm"]["http_address"]}:{app["evm"]["http_port"]}'
values = {
    "RELEASE_ID": "receipt-check",
    "RELEASE_TAG": "paxeer-network/v6.12.0",
    "RELEASE_SHA256": sys.argv[3],
    "SIDECAR_SHA256": sys.argv[4],
    "HALT_HEIGHT": "20",
    "CHAIN_ID": "swap_9000-1",
    "KEYRING_BACKEND": "test",
    "BIN_DIR": str(directory / "bin"),
    "OPERATOR_KEY_NAMES": "operator",
    "FULLNODE_HOMES": str(home),
    "SNAPSHOT_DIR": str(directory),
    "CREDENTIAL_DIR": str(home / "keyring-test"),
    "CHAINFLOW_KEY_FILE": str(home / "config/priv_validator_key.json"),
    "CONSENSUS_RPC": rpc,
    "EVM_RPC": evm,
    "DATA_SERVICE_URLS": rpc,
    "APP_URL": evm,
    "APP_BUILD_ID": "v6.12.0",
    "INVENTORY_FILE": str(directory / "stage-inputs"),
    "RECEIPT_DIR": str(directory),
}
for key in ("VALIDATOR_HOSTS", "FULLNODE_HOSTS", "ARCHIVE_HOST", "EDGE_HOST",
            "OPS_HOST", "BUILD_HOST"):
    values[key] = "localhost"
values["VALIDATOR_HOMES"] = str(home)
for key in ("VALIDATOR_UNITS", "FULLNODE_UNITS", "AUTOMATION_UNITS",
            "OLD_FEEDER_UNITS", "SIDECAR_UNITS"):
    values[key] = "paxd.service"
(directory / "stage-inputs").write_text(
    "".join(f"{key}={value}\n" for key, value in values.items())
)
PY
  (
    cd "$case_dir"
    bash "$root/tools/release/stage.sh" --env-file "$case_dir/stage-inputs" \
      --local --receipt "$case_dir/stage.json"
  )
  swap --role full --receipt "$case_dir/swap-full.json"
  swap --role full --verify --receipt "$case_dir/verify.json"
  python3 - "$case_dir" <<'PY'
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
stage = json.loads((root / "stage.json").read_text())
assert stage == {"hosts_staged": 1, "result": "pass"}, stage
expected = {"nodes_checked": 1, "nodes_passed": 1, "result": "pass"}
for name in ("swap-full.json", "verify.json"):
    assert json.loads((root / name).read_text()) == expected
PY
}

local_chain_swap_applies_plan() {
  setup_case local_chain
  new_home node 20
  VALIDATOR_HOMES=$node_home
  start_until_halt 20
  swap --role validator --halt-height 20
  assert_upgrade 21
  wait_height 23
  local rpc
  rpc=$(python3 - "$node_home/config/config.toml" <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as stream:
    print(tomllib.load(stream)["rpc"]["laddr"])
PY
)
  "$BIN_DIR/paxd" q upgrade applied v6.12.0 --home "$node_home" --node "$rpc" \
    > "$case_dir/applied.json"
  python3 - "$case_dir/applied.json" "$node_home/data/release-start.log" <<'PY'
import json
from pathlib import Path
import sys

applied = json.loads(Path(sys.argv[1]).read_text())
assert int(applied["header"]["height"]) == 21, applied
lines = Path(sys.argv[2]).read_text().splitlines()
assert sum("applying upgrade" in line and "v6.12.0" in line for line in lines) == 1
assert not any("panic" in line.lower() for line in lines)
PY
}

restart_binds_once() {
  setup_case restart
  new_home node 20
  FULLNODE_HOMES=$node_home
  start_until_halt 20
  swap --role full
  wait_height 23
  swap --role full
  swap --role full --verify
  python3 - "$node_home" <<'PY'
import json
from pathlib import Path
import sys
import tomllib
import urllib.request

home = Path(sys.argv[1])
text = (home / "data/release-start.log").read_text()
assert "address already in use" not in text.lower(), text[-4000:]
assert "panic" not in text.lower(), text[-4000:]
evm = tomllib.loads((home / "config/app.toml").read_text())["evm"]
request = urllib.request.Request(
    f'http://{evm["http_address"]}:{evm["http_port"]}',
    data=json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": "eth_chainId", "params": []}
    ).encode(),
    headers={"Content-Type": "application/json"},
)
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
with opener.open(request, timeout=3) as response:
    reply = json.load(response)
assert reply == {"jsonrpc": "2.0", "id": 1, "result": hex(9000)}, reply
PY
}

for scenario in swap_is_atomic_and_checked shared_binary_waits_for_halt upgrade_file_and_halt_reset \
  receipts_use_key_set local_chain_swap_applies_plan restart_binds_once; do
  set +e
  (
    set -e
    "$scenario"
  )
  status=$?
  set -e
  if ((status == 0)); then
    printf 'PASS %s\n' "$scenario"
    passed=$((passed + 1))
  else
    printf 'FAIL %s (exit %s)\n' "$scenario" "$status"
    failed=$((failed + 1))
  fi
  cleanup
done
printf 'swap: %s passed, %s failed\n' "$passed" "$failed"
printf 'swap: local-chain evidence %s\n' "$work"
((failed == 0))
