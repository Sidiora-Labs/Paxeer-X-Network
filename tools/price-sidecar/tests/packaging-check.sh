#!/usr/bin/env bash
# Offline packaging check for the price sidecar. It reads the systemd unit and
# the configuration template and asserts the unit's user, key credentials,
# key-file and path environment, restart policy, state directory and sandbox
# directives, and that the template carries field names, environment variable
# names and empty host fields only. It then builds the price-sidecar binary and
# runs price-sidecar --check-config on the template with every field filled but
# one, proving that the loader refuses each empty field by name. Nothing is
# installed and no network endpoint is contacted.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
INSTALL_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../../.." && pwd)
UNIT="$INSTALL_DIR/price-sidecar.service"
TEMPLATE="$INSTALL_DIR/config.template.json"
MANIFEST="$REPO_ROOT/platform/Cargo.toml"

fail() { printf 'packaging-check: error: %s\n' "$*" >&2; exit 1; }
pass() { printf 'packaging-check: ok: %s\n' "$*"; }

for file in "$UNIT" "$TEMPLATE" "$MANIFEST"; do
    [ -f "$file" ] || fail "missing $file"
done

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
chmod 0700 "$WORK"

# has_line TEXT LINE WHAT: TEXT holds LINE as a whole line.
has_line() {
    grep -Fxq -- "$2" <<<"$1" || fail "$3: expected the line '$2'"
}

# lacks_match TEXT PATTERN WHAT: no line of TEXT matches the extended regex.
lacks_match() {
    if grep -Eq -- "$2" <<<"$1"; then
        fail "$3: unexpected line matching '$2'"
    fi
}

# ------------------------------------------------------------ unit_directives
SERVICE=$(awk '/^\[/{inside = ($0 == "[Service]"); next} inside && NF {print}' "$UNIT")
[ -n "$SERVICE" ] || fail "unit: no [Service] section"
has_line "$(cat "$UNIT")" "WantedBy=multi-user.target" "unit install target"
for directive in \
    "Type=simple" \
    "User=price-sidecar" \
    "Group=price-sidecar" \
    "LoadCredential=feed.key:/etc/price-sidecar/keys/feed.key" \
    "LoadCredential=chainflow.key:/etc/price-sidecar/keys/chainflow.key" \
    "Environment=PRICE_SIDECAR_FEED_KEY_FILE=%d/feed.key" \
    "Environment=PRICE_FEED_REST_API_KEY_FILE=%d/chainflow.key" \
    "Environment=PRICE_SIDECAR_CONFIG=/etc/price-sidecar/config.json" \
    "Environment=PRICE_SIDECAR_STATE_DIR=%S/price-sidecar" \
    "EnvironmentFile=/etc/price-sidecar/endpoints.env" \
    "ExecStart=/usr/local/bin/price-sidecar" \
    "Restart=on-failure" \
    "StateDirectory=price-sidecar" \
    "StateDirectoryMode=0700" \
    "UMask=0077" \
    "NoNewPrivileges=yes" \
    "ProtectSystem=strict" \
    "ProtectHome=yes" \
    "PrivateTmp=yes" \
    "PrivateDevices=yes" \
    "ProtectKernelTunables=yes" \
    "ProtectKernelModules=yes" \
    "ProtectKernelLogs=yes" \
    "ProtectControlGroups=yes" \
    "ProtectClock=yes" \
    "ProtectHostname=yes" \
    "ProtectProc=invisible" \
    "ProcSubset=pid" \
    "RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX" \
    "RestrictNamespaces=yes" \
    "RestrictRealtime=yes" \
    "RestrictSUIDSGID=yes" \
    "LockPersonality=yes" \
    "MemoryDenyWriteExecute=yes" \
    "RemoveIPC=yes" \
    "SystemCallArchitectures=native" \
    "SystemCallFilter=@system-service" \
    "SystemCallFilter=~@privileged @resources" \
    "CapabilityBoundingSet=" \
    "AmbientCapabilities="; do
    has_line "$SERVICE" "$directive" "unit"
done
[ "$(grep -c '^LoadCredential=' <<<"$SERVICE")" -eq 2 ] || fail "unit: expected exactly the two LoadCredential lines"
[ "$(grep -c '^Environment=' <<<"$SERVICE")" -eq 4 ] || fail "unit: expected exactly the four Environment lines"
[ "$(grep -c '^EnvironmentFile=' <<<"$SERVICE")" -eq 1 ] || fail "unit: expected exactly one EnvironmentFile line"
lacks_match "$SERVICE" '^(User|Group)=(root|0)$' "unit"
lacks_match "$SERVICE" '^(DynamicUser|PermissionsStartOnly)=' "unit"
lacks_match "$SERVICE" '^ExecStart=[+!-]' "unit runs ExecStart with full privileges"
lacks_match "$SERVICE" '^Environment=PRICE_SIDECAR_(CONSENSUS_RPC_URL|EVM_RPC_URL|HEALTH_ADDR)=' "unit carries a host value"
lacks_match "$(cat "$UNIT")" '://[^/]*[0-9]|[0-9]{1,3}(\.[0-9]{1,3}){3}' "unit carries a host value"
pass "unit_directives: dedicated user, feed and Chainflow key credentials, %d key paths, restart, state directory 0700, sandbox"

# -------------------------------------------- template_names_only and loader
: "${CARGO_TARGET_DIR:=$REPO_ROOT/platform/target}"
export CARGO_TARGET_DIR
cargo build --locked --manifest-path "$MANIFEST" \
    --package layerx-oracle-feeder --bin price-sidecar >&2
LOADER="$CARGO_TARGET_DIR/debug/price-sidecar"
[ -x "$LOADER" ] || fail "price-sidecar was not built at $LOADER"

python3 - "$LOADER" "$TEMPLATE" "$WORK" <<'PY'
import copy
import json
import os
import re
import subprocess
import sys

loader, template_path, work = sys.argv[1:4]


def fail(message):
    sys.stderr.write(f"packaging-check: error: {message}\n")
    sys.exit(1)


def ok(message):
    print(f"packaging-check: ok: {message}")


with open(template_path, encoding="utf-8") as handle:
    text = handle.read()
template = json.loads(text)

# template_names_only
variables = [
    "PRICE_SIDECAR_CONFIG",
    "PRICE_SIDECAR_FEED_KEY_FILE",
    "PRICE_FEED_REST_API_KEY_FILE",
    "PRICE_SIDECAR_STATE_DIR",
    "PRICE_SIDECAR_HEALTH_ADDR",
    "PRICE_SIDECAR_CONSENSUS_RPC_URL",
    "PRICE_SIDECAR_EVM_RPC_URL",
]
for name in variables:
    if name not in text:
        fail(f"template: the variable name {name} does not appear")
if "://" in text:
    fail("template: carries a scheme separator")
if re.search(r"0x[0-9a-fA-F]{2,}|[0-9a-fA-F]{40}", text):
    fail("template: carries an address literal")
if re.search(r"\b\d{1,3}(\.\d{1,3}){3}\b|\[[0-9a-fA-F:]*:[0-9a-fA-F:]*\]", text):
    fail("template: carries a network address literal")
if re.search(r"[0-9a-fA-F]{64}|[A-Za-z0-9+/]{43}=", text):
    fail("template: carries a key value")
top = ["note", "chain_id", "validator", "feed_account", "gas_limit", "cadence_ms",
       "retry_backoff_ms", "retry_max_attempts", "chainflow", "markets"]
if list(template) != top:
    fail(f"template: the fields are {list(template)}, expected {top}")
chainflow = ["base_url", "api_key_env", "timeout_ms", "max_quote_age_ms"]
if list(template["chainflow"]) != chainflow:
    fail(f"template: the chainflow fields are {list(template['chainflow'])}, expected {chainflow}")
if template["chainflow"]["api_key_env"] != "PRICE_FEED_REST_API_KEY":
    fail("template: chainflow.api_key_env does not name PRICE_FEED_REST_API_KEY")
symbols = ["BTC", "ETH", "SOL", "AVAX", "LINK", "TSLA", "NVDA", "NAS100", "XAU", "SPX500",
           "GOOGL", "PAX", "SID", "HYPE", "XRP", "ASTER", "TRUMP", "BNB"]
expected_markets = [{"market_id": i, "symbol": s} for i, s in enumerate(symbols)]
if template["markets"] != expected_markets:
    fail("template: markets are not the eighteen pricefeed markets in id order")
host_fields = [
    (("chain_id",), "chain_id"),
    (("validator",), "validator"),
    (("feed_account",), "feed_account"),
    (("chainflow", "base_url"), "chainflow.base_url"),
]


def get(document, path):
    for part in path:
        document = document[part]
    return document


def put(document, path, value):
    for part in path[:-1]:
        document = document[part]
    document[path[-1]] = value


for path, field in host_fields:
    if get(template, path) != "":
        fail(f"template: {field} is not empty")
ok("template_names_only: field names, variable names and empty host fields only")

# loader_refuses_placeholders
environment = {
    name: value
    for name, value in os.environ.items()
    if not name.startswith("PRICE_SIDECAR_") and not name.startswith("PRICE_FEED_REST_API_KEY")
}
filled = {
    "chain_id": "check-1",
    "validator": "0x" + "01" * 20,
    "feed_account": "02" * 20,
    "chainflow.base_url": "https://chainflow.invalid",
}


def run(config, step):
    path = os.path.join(work, f"config-{step}.json")
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(config, handle)
    result = subprocess.run(
        [loader, "--check-config"],
        env={**environment, "PRICE_SIDECAR_CONFIG": path},
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
        timeout=60,
        check=False,
    )
    return result.returncode, result.stdout.strip(), result.stderr.strip()


code, _, stderr = run(template, "template")
if code != 1 or not stderr.startswith("price-sidecar: config: unknown field `note`"):
    fail(f"template: expected exit 1 refusing the note, got exit {code} and '{stderr}'")
ok("loader refused the template's note")

base = copy.deepcopy(template)
del base["note"]
for empty_path, empty_field in host_fields:
    config = copy.deepcopy(base)
    for path, field in host_fields:
        if field != empty_field:
            put(config, path, filled[field])
    code, _, stderr = run(config, empty_field)
    expected = f"price-sidecar: config field {empty_field} is empty"
    if code != 1 or stderr != expected:
        fail(f"{empty_field} left empty: expected exit 1 and '{expected}', got exit {code} and '{stderr}'")
    ok(f"loader_refuses_placeholders: {expected}")

config = copy.deepcopy(base)
for path, field in host_fields:
    put(config, path, filled[field])
code, stdout, stderr = run(config, "filled")
if code != 0 or stdout != "price-sidecar: config ok":
    fail(f"filled template: expected exit 0 and 'price-sidecar: config ok', got exit {code}, '{stdout}' and '{stderr}'")
ok("loader accepted the template with every host field filled")
PY

pass "price-sidecar packaging"
