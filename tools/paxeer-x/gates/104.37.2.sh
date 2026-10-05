#!/usr/bin/env bash
# paxeer-x-services: hpx
set -euo pipefail
if (($#)); then
	echo "usage: $0 (no arguments)" >&2
	exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
root=$(pwd -P)
export PATH="/usr/local/go/bin:$PATH" LC_ALL=C

: "${PAXEER_X_EVIDENCE_DIR:?private evidence directory required}"
: "${PAXEER_X_HPX_PAXD:?verified local paxd runtime binary required}"
: "${PAXEER_X_HPX_RUNTIME_CONFIG:?local Paxeer runtime configuration directory required}"
origin="${PAXEER_X_HPX_ORIGIN:-https://node.hyperpaxeer.com}"
image="${PAXEER_X_HPX_CLEAN_IMAGE:-ubuntu:24.04}"
chain="hyperpax_125-1"
revision=$(git rev-parse HEAD)

work=$(mktemp -d "$PAXEER_X_EVIDENCE_DIR/hpx-104.37.2.XXXXXXXX")
chmod 0700 "$work"
pids=()
cleanup() {
	for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
	wait 2>/dev/null || true
}
trap cleanup EXIT
tests=0
pass() { tests=$((tests + 1)); printf 'ok %d %s\n' "$tests" "$*"; }
fail() { printf 'hpx gate FAIL: %s (evidence %s)\n' "$*" >&2; exit 1; }
sha() { sha256sum "$1" | awk '{print $1}'; }
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
status_of() { curl -sS -o /dev/null -w '%{http_code}' "$@"; }

# ac_1 / do_2: monorepo-derived sources, no machine-specific checkout or obsolete origin.
if grep -rnE '/root/(Layerx-protocol|project-Quorum|lx-)|project-Quorum|paxeer-network/|paxeer\.app|sidiora\.xyz' \
	hpx .github/workflows/paxeer-hpx-registry.yml docker/hpx-registry; then
	fail "HPX publication references a private checkout or obsolete origin"
fi
for file in hpx/get-hpx.sh hpx/hpx hpx/stake-fleet.sh hpx/fleet-keygen.sh; do
	grep -qF 'MIRROR="${HPX_MIRROR:-https://node.hyperpaxeer.com}"' "$file" || fail "$file default mirror is not node.hyperpaxeer.com"
done
grep -qF 'HPX_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"' hpx/publish.sh || fail "publish.sh does not derive its source root from hpx"
grep -q 'HPX_RUNTIME_CONFIG_DIR' hpx/publish.sh && grep -q 'HPX_ARTIFACTS_ROOT' hpx/publish.sh || fail "publish.sh lacks runtime-config or artifact-root overrides"
pass "publication sources derive from the monorepo with explicit overrides"

# ac_4 / ac_6: the registry's real handlers, persistence, address boundary and static surface.
(cd hpx/registry && go vet ./... && go test -count=1 -v ./...) > "$work/registry-test.log" 2>&1 || fail "registry tests failed"
if grep -qE -- '--- (FAIL|SKIP)' "$work/registry-test.log"; then fail "registry tests failed or skipped"; fi
registry_tests=$(grep -c -- '--- PASS' "$work/registry-test.log")
[ "$registry_tests" -ge 7 ] || fail "registry test corpus incomplete"
tests=$((tests + registry_tests))
echo "ok registry go tests $registry_tests"
(cd hpx/registry && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.sourceRevision=$revision" -o "$work/hpx-registry" .) \
	> "$work/registry-build.log" 2>&1 || fail "revision-bound registry build failed"
pass "registry executable built from hpx/registry at $revision"
grep -qxF 'User=hpx-registry' hpx/hosting/deploy.sh && grep -qxF 'HPX_ADDR=127.0.0.1:8099' hpx/hosting/deploy.sh &&
	grep -qxF 'ReadWritePaths=${DATA_DIR}' hpx/hosting/deploy.sh && grep -qxF 'Restart=on-failure' hpx/hosting/deploy.sh &&
	grep -qF 'sha256sum -c "$asset.sha256"' hpx/hosting/deploy.sh || fail "production service is not unprivileged, loopback, durable and checksum-bound"
grep -qF 'ssl_certificate /etc/letsencrypt/live/node.hyperpaxeer.com/fullchain.pem;' hpx/hosting/nginx.conf &&
	grep -qF 'limit_req zone=hpx_register' hpx/hosting/nginx.conf && grep -qF 'proxy_set_header X-Real-IP $remote_addr;' hpx/hosting/nginx.conf &&
	! grep -q 'autoindex on' hpx/hosting/nginx.conf || fail "Nginx HTTPS boundary incomplete"
grep -qF 'main.sourceRevision=${SOURCE_REVISION}' .github/workflows/paxeer-hpx-registry.yml &&
	grep -qF 'hpx-registry-${{ github.sha }}' .github/workflows/paxeer-hpx-registry.yml || fail "registry automation is not revision-bound"
pass "loopback systemd service, Nginx HTTPS proxy and revision-bound automation declared"

# ac_2: assemble immutable releases from the real local runtime inputs.
art="$work/artifacts"
publish() {
	(cd / && env -u SRC_CFG -u WASM_ROOT SRC_BIN="$PAXEER_X_HPX_PAXD" HPX_RUNTIME_CONFIG_DIR="$PAXEER_X_HPX_RUNTIME_CONFIG" \
		HPX_ARTIFACTS_ROOT="$art" HPX_RELEASE_ID="$1" "${@:2}" bash "$root/hpx/publish.sh")
}
publish gate-a > "$work/publish-a.log" 2>&1 || fail "publication of the first release failed"
[ "$(readlink "$art/current")" = releases/gate-a ] || fail "current pointer does not name the assembled release"
release="$art/releases/gate-a"
expected="chain-info.json checksums.txt config/fullnode/app.toml config/fullnode/config.toml config/validator/app.toml config/validator/config.toml genesis.json get-hpx.sh hpx install.sh lib/libwasmvm.aarch64.so lib/libwasmvm.x86_64.so lib/libwasmvm152.aarch64.so lib/libwasmvm152.x86_64.so lib/libwasmvm155.aarch64.so lib/libwasmvm155.x86_64.so paxd paxd.sha256 uninstall.sh"
[ "$(cd "$release" && find . -type f -printf '%P\n' | sort | tr '\n' ' ' | sed 's/ $//')" = "$expected" ] || fail "release contents incomplete"
(cd "$release" && sha256sum -c --quiet checksums.txt) || fail "release manifest does not verify"
awk '{print $2}' "$release/checksums.txt" | sort -c || fail "manifest is not sorted"
[ "$(awk '{print $2}' "$release/checksums.txt" | tr '\n' ' ' | sed 's/ $//')" = "${expected/checksums.txt /}" ] || fail "manifest does not cover every artifact"
jq -e --arg c "$chain" --arg p "$(sha "$PAXEER_X_HPX_PAXD")" --arg r "$revision" \
	'.chain_id == $c and .paxd_sha256 == $p and .source_revision == $r and .release_id == "gate-a" and (.seeds | length > 0)' \
	"$release/chain-info.json" > /dev/null || fail "chain metadata mismatch"
[ "$(sha "$release/genesis.json")" = "$(sha "$PAXEER_X_HPX_RUNTIME_CONFIG/genesis.json")" ] || fail "genesis not copied from runtime config"
grep -q '^mode = "validator"' "$release/config/validator/config.toml" && grep -q '^mode = "full"' "$release/config/fullnode/config.toml" || fail "node configurations not specialised"
[ -z "$(find "$release" -perm -u+w -print -quit)" ] || fail "release is writable after publication"
pass "release assembled with paxd, all libwasmvm runtimes, configs, CLI, metadata and sorted manifest"

wasm="$work/wasm-incomplete"
mkdir -p "$wasm/wasm-runtime/internal/api" "$wasm/wasm/x/wasm/artifacts/v152/api" "$wasm/wasm/x/wasm/artifacts/v155/api"
cp wasm-runtime/internal/api/libwasmvm.*.so "$wasm/wasm-runtime/internal/api/"
cp wasm/x/wasm/artifacts/v152/api/libwasmvm152.*.so "$wasm/wasm/x/wasm/artifacts/v152/api/"
cp wasm/x/wasm/artifacts/v155/api/libwasmvm155.x86_64.so "$wasm/wasm/x/wasm/artifacts/v155/api/"
if publish gate-missing WASM_ROOT="$wasm" > "$work/publish-missing.log" 2>&1; then fail "publication accepted a missing native library"; fi
grep -q 'required input not found: .*libwasmvm155.aarch64.so' "$work/publish-missing.log" || fail "missing library not named"
if publish gate-a > "$work/publish-duplicate.log" 2>&1; then fail "publication overwrote an existing release"; fi
[ "$(readlink "$art/current")" = releases/gate-a ] && [ ! -e "$art/releases/gate-missing" ] &&
	[ -z "$(find "$art/releases" -maxdepth 1 -name '.stage.*' -print -quit)" ] || fail "failed publication changed the served release"
publish gate-b > "$work/publish-b.log" 2>&1 || fail "second publication failed"
[ "$(readlink "$art/current")" = releases/gate-b ] && (cd "$release" && sha256sum -c --quiet checksums.txt) || fail "pointer switch or prior release immutability failed"
pass "incomplete or duplicate publication fails closed and the pointer switches only after assembly"

# ac_3 / ac_4: the built registry process on loopback serving the published release.
start_registry() {
	local port="$1" artifacts="$2" data="$3" log="$4"
	HPX_ADDR="127.0.0.1:$port" HPX_ARTIFACTS_DIR="$artifacts" HPX_DATA_DIR="$data" HPX_CHAIN_ID="$chain" \
		HPX_SEED_PEERS="$(jq -r '.seeds | join(",")' "$art/current/chain-info.json")" HPX_REGISTER_TOKEN= \
		"$work/hpx-registry" >> "$log" 2>&1 &
	pids+=("$!")
	for _ in $(seq 1 50); do
		curl -fsS "http://127.0.0.1:$port/healthz" > /dev/null 2>&1 && return 0
		sleep 0.1
	done
	fail "registry did not start on 127.0.0.1:$port"
}
if HPX_ADDR="0.0.0.0:$(free_port)" HPX_DATA_DIR="$work/refused" timeout 10 "$work/hpx-registry" > "$work/registry-public-bind.log" 2>&1; then
	fail "registry accepted a public listen address"
fi
grep -q 'must be a loopback address' "$work/registry-public-bind.log" || fail "public bind refusal not reported"
port=$(free_port)
mirror="http://127.0.0.1:$port"
data="$work/registry-data"
start_registry "$port" "$art/current" "$data" "$work/registry.log"
registry_pid=${pids[-1]}
curl -fsS "$mirror/healthz" | jq -e --arg c "$chain" --arg r "$revision" '.ok and .chain_id == $c and .source_revision == $r' > /dev/null || fail "health identity"
while read -r want rel; do
	curl -fsS "$mirror/$rel" -o "$work/served"
	[ "$(sha "$work/served")" = "$want" ] || fail "served $rel differs from manifest"
done < "$art/current/checksums.txt"
for path in / /lib/ /config/ /releases/ /current /data/registry.json; do
	[ "$(status_of "$mirror$path")" = 404 ] || fail "undeclared path $path is served"
done
pass "every manifest artifact served byte-exact; listings and undeclared paths unavailable"
node="0123456789abcdef0123456789abcdef01234567"
register() { curl -sS -o "$work/register.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' "$@" "$mirror/api/register"; }
[ "$(register -H 'X-Real-IP: 203.0.113.7' -d "{\"node_id\":\"$node\",\"ip\":\"8.8.8.8\",\"p2p_port\":26656}")" = 200 ] || fail "valid proxied registration refused"
[ "$(register -H 'X-Forwarded-For: 203.0.113.9' -d '{"node_id":"fedcba9876543210fedcba9876543210fedcba98"}')" = 400 ] || fail "spoofed forwarding header accepted"
[ "$(register -H 'X-Real-IP: 203.0.113.7' -d '{"node_id":"not-a-node"}')" = 400 ] || fail "malformed node id accepted"
[ "$(register -H 'X-Real-IP: 10.0.0.8' -d "{\"node_id\":\"$node\"}")" = 400 ] || fail "private address accepted"
[ "$(register -H 'X-Real-IP: 203.0.113.7' -d "{\"node_id\":\"$node\",\"p2p_port\":65536}")" = 400 ] || fail "invalid port accepted"
[ "$(status_of "$mirror/api/register")" = 405 ] || fail "GET registration accepted"
[ "$(curl -fsS -H 'X-Real-IP: 203.0.113.7' "$mirror/api/myip")" = 203.0.113.7 ] || fail "caller address is not the proxy-observed address"
pass "registration accepts only proxy-observed public addresses and rejects malformed and spoofed input"
kill "$registry_pid"
wait "$registry_pid" 2>/dev/null || true
start_registry "$port" "$art/current" "$data" "$work/registry.log"
curl -fsS "$mirror/api/peers" | jq -e --arg p "$node@203.0.113.7:26656" '.peers | index($p) != null' > /dev/null || fail "restart lost persisted peers"
curl -fsS "$mirror/api/nodes" | jq -e '.count == 1 and .nodes[0].ip == "203.0.113.7"' > /dev/null || fail "restart lost persisted nodes"
pass "registry restart recovers persisted peers from the external data directory"
codes=""
for i in $(seq 1 11); do
	codes="$codes $(register -H 'X-Real-IP: 203.0.113.20' -d "{\"node_id\":\"$(printf '%040x' "$i")\"}")"
done
[ "$codes" = "$(printf ' 200%.0s' $(seq 1 10)) 429" ] || fail "registration is not rate limited:$codes"
for path in api/peers api/peers.txt api/nodes api/statesync; do
	[ "$(status_of "$mirror/$path")" = 200 ] || fail "$path unavailable"
done
pass "registration is rate limited and the public API surface is served"

# ac_5: the installer and node manager fail closed in a clean host.
variant() {
	local name="$1"
	cp -a "$art/releases/gate-b" "$art/releases/$name"
	chmod -R u+w "$art/releases/$name"
}
variant tampered-paxd
printf 'x' >> "$art/releases/tampered-paxd/paxd"
variant missing-lib
rm "$art/releases/missing-lib/lib/libwasmvm155.x86_64.so"
variant tampered-config
printf '\n# altered\n' >> "$art/releases/tampered-config/config/fullnode/config.toml"
declare -A mirrors=()
for name in tampered-paxd missing-lib tampered-config; do
	p=$(free_port)
	start_registry "$p" "$art/releases/$name" "$work/data-$name" "$work/registry-$name.log"
	mirrors[$name]="http://127.0.0.1:$p"
done
cat > "$work/clean-host.sh" <<'CLEAN'
set -u
export DEBIAN_FRONTEND=noninteractive HPX_TYPE=fullnode
apt-get update -qq > /dev/null && apt-get install -y -qq curl jq ca-certificates util-linux > /dev/null || { echo "dependency install failed"; exit 1; }
installed() { ls /usr/local/bin/paxd /usr/lib/x86_64-linux-gnu/libwasmvm*.so /root/.paxeer/config/config.toml 2> /dev/null; }
reset() { rm -f /usr/local/bin/hpx /usr/local/bin/paxd /usr/lib/x86_64-linux-gnu/libwasmvm*.so; rm -rf /root/.paxeer; }
case_run() {
	local name="$1" mirror="$2" expect="$3" arch="$4" out
	reset
	out=$(curl -fsSL "$mirror/get-hpx.sh" | HPX_MIRROR="$mirror" $arch bash 2>&1); code=$?
	printf '%s\n' "$out" > "/evidence/clean-$name.log"
	[ "$code" -ne 0 ] || { echo "FAIL $name exit 0"; return; }
	printf '%s' "$out" | grep -q -- "$expect" || { echo "FAIL $name missing '$expect'"; return; }
	echo "ok $name"
}
case_run tampered-paxd "$TAMPERED_PAXD" 'paxd sha256 mismatch' ''
[ -z "$(installed)" ] || echo "FAIL tampered-paxd installed $(installed)"
case_run missing-lib "$MISSING_LIB" 'lib/libwasmvm155.x86_64.so download failed' ''
[ -z "$(installed)" ] || echo "FAIL missing-lib installed $(installed)"
case_run tampered-config "$TAMPERED_CONFIG" 'config/fullnode/config.toml sha256 mismatch' ''
! grep -qs '^# altered$' /root/.paxeer/config/config.toml && [ ! -e /root/.paxeer/config/config.toml.new ] && [ ! -e /root/.paxeer/config/genesis.json ] || echo "FAIL tampered-config installed configuration"
case_run unsupported-arch "$TAMPERED_PAXD" 'unsupported arch' 'setarch i686'
[ -z "$(installed)" ] || echo "FAIL unsupported-arch installed $(installed)"
reset
curl -fsSL "$GOOD/get-hpx.sh" -o /tmp/get-hpx.sh && HPX_MIRROR="$GOOD" bash /tmp/get-hpx.sh < /dev/null > /evidence/clean-cli.log 2>&1
[ "$(sha256sum /usr/local/bin/hpx | cut -d' ' -f1)" = "$(curl -fsS "$GOOD/checksums.txt" | awk '$2 == "hpx" {print $1}')" ] && echo "ok verified-cli" || echo "FAIL verified-cli"
rm -f /usr/local/bin/paxd /usr/lib/x86_64-linux-gnu/libwasmvm*.so
rm -rf /root/.paxeer
out=$(HPX_MIRROR="$GOOD" setarch i686 hpx setup 2>&1 < /dev/null); code=$?
printf '%s\n' "$out" > /evidence/clean-setup-arch.log
[ "$code" -ne 0 ] && printf '%s' "$out" | grep -q 'unsupported architecture' && [ ! -e /usr/local/bin/paxd ] && echo "ok node-manager-arch" || echo "FAIL node-manager-arch"
CLEAN
docker run --rm --network host -v "$work:/evidence" -e GOOD="$mirror" -e TAMPERED_PAXD="${mirrors[tampered-paxd]}" \
	-e MISSING_LIB="${mirrors[missing-lib]}" -e TAMPERED_CONFIG="${mirrors[tampered-config]}" \
	"$image" bash /evidence/clean-host.sh > "$work/clean-host.log" 2>&1 || fail "clean host run failed"
cat "$work/clean-host.log"
if grep -q '^FAIL' "$work/clean-host.log"; then fail "installer fail-closed contract"; fi
clean_ok=$(grep -c '^ok ' "$work/clean-host.log")
[ "$clean_ok" = 6 ] || fail "clean host corpus incomplete"
tests=$((tests + clean_ok))

# ac_3 / ac_6: the public origin, its certificate and the deployed registry revision.
make --no-print-directory hpx-public-check HPX_ORIGIN="$origin" > "$work/hpx-public-check.log" 2>&1 || fail "make hpx-public-check"
pass "retained public check against $origin"
host=${origin#https://}
python3 - "$host" > "$work/certificate.log" 2>&1 <<'PY' || fail "public certificate"
import socket, ssl, sys, time
host = sys.argv[1]
context = ssl.create_default_context()
with socket.create_connection((host, 443), timeout=15) as raw, context.wrap_socket(raw, server_hostname=host) as tls:
    cert = tls.getpeercert()
remaining = ssl.cert_time_to_seconds(cert['notAfter']) - time.time()
assert remaining > 7 * 86400, 'certificate expires within seven days'
print(host, cert['notAfter'])
PY
pass "certificate valid for $host"
curl -fsS "$origin/checksums.txt" -o "$work/public-checksums.txt"
while read -r want rel; do
	curl -fsS "$origin/$rel" -o "$work/public-artifact"
	[ "$(sha "$work/public-artifact")" = "$want" ] || fail "public $rel differs from its manifest"
done < "$work/public-checksums.txt"
for path in api/peers api/peers.txt api/nodes api/myip api/statesync install uninstall; do
	[ "$(status_of "$origin/$path")" = 200 ] || fail "public $path unavailable"
done
for path in /lib/ /config/ /releases/ /current /registry.json; do
	[ "$(status_of "$origin$path")" = 404 ] || fail "public undeclared path $path is served"
done
[ "$(status_of "$origin/api/register")" = 405 ] || fail "public GET registration accepted"
[ "$(status_of -X POST -H 'Content-Type: application/json' -d '{"node_id":"not-a-node"}' "$origin/api/register")" = 400 ] || fail "public malformed registration accepted"
pass "public origin serves manifest-bound artifacts and APIs and rejects malformed registration"
live=$(curl -fsS "$origin/healthz" | jq -er '.source_revision')
[[ "$live" =~ ^[0-9a-f]{40}$ ]] && git cat-file -e "$live^{commit}" 2> /dev/null || fail "deployed registry revision $live is not a known source revision"
git diff --quiet "$live" HEAD -- hpx/registry || fail "deployed registry revision $live does not carry this candidate's hpx/registry"
pass "deployed registry is the revision-bound build of this candidate"

# ac_7: a clean host installs through the public HTTPS command and its node reaches the declared chain.
[ "${PAXEER_X_HPX_PUBLIC_INSTALL:-}" = owner-approved ] || fail "clean-host public install registers a production peer; owner approval PAXEER_X_HPX_PUBLIC_INSTALL=owner-approved required"
cat > "$work/public-host.sh" <<'PUBLIC'
set -u
export DEBIAN_FRONTEND=noninteractive HPX_TYPE=fullnode
apt-get update -qq > /dev/null && apt-get install -y -qq curl ca-certificates > /dev/null || exit 1
curl -sSL "$ORIGIN/get-hpx.sh" | bash > /evidence/public-install.log 2>&1
curl -fsS "$ORIGIN/checksums.txt" -o /tmp/checksums.txt || exit 1
check() { [ "$(sha256sum "$1" | cut -d' ' -f1)" = "$(awk -v p="$2" '$2 == p {print $1}' /tmp/checksums.txt)" ] && echo "ok public $2" || echo "FAIL public $2"; }
check /usr/local/bin/hpx hpx
check /usr/local/bin/paxd paxd
for lib in libwasmvm.x86_64.so libwasmvm152.x86_64.so libwasmvm155.x86_64.so; do check "/usr/lib/x86_64-linux-gnu/$lib" "lib/$lib"; done
check /root/.paxeer/config/genesis.json genesis.json
check /root/.paxeer/config/app.toml config/fullnode/app.toml
paxd start --home /root/.paxeer > /evidence/public-node.log 2>&1 &
for _ in $(seq 1 180); do
	status=$(curl -fsS --max-time 3 http://127.0.0.1:26657/status 2> /dev/null) || { sleep 5; continue; }
	network=$(printf '%s' "$status" | jq -r '(.result // .).node_info.network')
	height=$(printf '%s' "$status" | jq -r '(.result // .).sync_info.latest_block_height')
	if [ "$network" = "$CHAIN" ] && [ "${height:-0}" -gt 0 ]; then echo "ok public node reached $CHAIN at $height"; exit 0; fi
	sleep 5
done
echo "FAIL public node did not reach $CHAIN"
PUBLIC
docker run --rm -v "$work:/evidence" -e ORIGIN="$origin" -e CHAIN="$chain" "$image" bash /evidence/public-host.sh \
	> "$work/public-host.log" 2>&1 || fail "public clean host run failed"
cat "$work/public-host.log"
if grep -q '^FAIL' "$work/public-host.log"; then fail "public clean-host install"; fi
public_ok=$(grep -c '^ok ' "$work/public-host.log")
[ "$public_ok" = 8 ] || fail "public clean-host corpus incomplete"
tests=$((tests + public_ok))
printf 'PAXEER_X_GATE tests=%d skipped=0\n' "$tests"
