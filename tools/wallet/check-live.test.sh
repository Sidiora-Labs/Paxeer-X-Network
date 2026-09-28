#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root/tools/wallet/check-live.sh"
work="$(mktemp -d)"
responder_pid=""

cleanup() {
	if [ -n "$responder_pid" ]; then
		kill "$responder_pid" 2>/dev/null || true
		wait "$responder_pid" 2>/dev/null || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

if ! command -v node >/dev/null 2>&1; then
	echo "check-live.test: node is required" >&2
	exit 2
fi

cat >"$work/responder.mjs" <<'JS'
import http from "node:http";
import fs from "node:fs";

const [portFile] = process.argv.slice(2);
const profiles = {
  good: {},
  wrong_chain: { eth_chainId: { result: "0x1" } },
  kernel_reachable: {
    px_getNetwork: {
      result: {
        network_id: "local",
        paxeer: { chain_id: "0x7d", latest_block: "0x10" },
        kernel: { available: true, reason: "available" },
      },
    },
  },
  kernel_data: {
    lx_getAccount: { result: { name: "account", balance: "1", next_sequence: 1 } },
  },
  resolve_error: {
    px_resolveAccount: {
      error: { code: -32001, message: "Paxeer read unavailable", data: { code: "paxeer_unreachable" } },
    },
  },
};
const base = {
  eth_chainId: { result: "0x7d" },
  px_resolveAccount: {
    result: {
      evm_address: "0x000000000000000000000000000000000000dead",
      pax_address: null,
      layerx_did: null,
      layerx_account: null,
      bound: false,
    },
  },
  px_getNetwork: {
    result: {
      network_id: "local",
      paxeer: { chain_id: "0x7d", latest_block: "0x10" },
      kernel: { available: false, reason: "not_configured" },
    },
  },
  lx_getAccount: {
    error: {
      code: -32010,
      message: "Kernel unavailable",
      data: { code: "kernel_unavailable", backend: "public_core", reason: "not_configured" },
    },
  },
};

const health = (id, profile) => {
  const peers = {};
  for (const peer of ["1", "2", "3", "4", "5"]) {
    if (peer !== id) peers[peer] = { reachable: true, rtt_ns: 1500000 };
  }
  const report = {
    node_id: id,
    region: "r" + id,
    share_count: 2,
    refresh_epoch: 1,
    audit_sequence: 7,
    audit_head: "ab".repeat(32),
    peers,
    reachable_peers: 4,
    ready: true,
  };
  if (profile === "unready") {
    report.ready = false;
    report.readiness_error = "audit log broken";
  }
  if (profile === "peerdown") {
    const down = id === "5" ? "1" : "5";
    peers[down] = { reachable: false, rtt_ns: 0 };
    report.reachable_peers = 3;
  }
  return report;
};

const server = http.createServer((request, response) => {
  const node = request.url.match(/^\/attest_([a-z]+)\/([1-5])\/health$/);
  if (node) {
    if (request.method !== "GET" || !["ready", "unready", "peerdown"].includes(node[1])) {
      response.writeHead(404, { "content-type": "application/json" });
      response.end('{"ok":false}');
      return;
    }
    const report = health(node[2], node[1]);
    response.writeHead(report.ready ? 200 : 503, { "content-type": "application/json" });
    response.end(JSON.stringify(report));
    return;
  }
  const [, profile, rest] = request.url.match(/^\/([a-z_]+)(\/.*)$/) || [];
  if (request.method !== "POST" || rest !== "/rpc" || !(profile in profiles)) {
    response.writeHead(404, { "content-type": "application/json" });
    response.end('{"ok":false}');
    return;
  }
  let body = "";
  request.on("data", (chunk) => (body += chunk));
  request.on("end", () => {
    let call;
    try {
      call = JSON.parse(body);
    } catch {
      response.writeHead(400, { "content-type": "application/json" });
      response.end('{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}');
      return;
    }
    const answer = profiles[profile][call.method] || base[call.method] || {
      error: { code: -32601, message: "Method not found" },
    };
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ jsonrpc: "2.0", id: call.id, ...answer }));
  });
});
server.listen(0, "127.0.0.1", () => {
  fs.writeFileSync(portFile, String(server.address().port));
});
JS

node "$work/responder.mjs" "$work/port" &
responder_pid=$!
for _ in $(seq 100); do
	[ -s "$work/port" ] && break
	sleep 0.05
done
if [ ! -s "$work/port" ]; then
	echo "check-live.test: responder did not start" >&2
	exit 2
fi
port="$(cat "$work/port")"

failures=0

run_checker() {
	CHECK_LIVE_ENDPOINT_BASE="$1" CHECK_LIVE_TIMEOUT=5 "$checker" "${@:2}" 2>&1
}

expect() {
	local name="$1" profile="$2" want_status="$3" output status=0
	shift 3
	output="$(run_checker "http://127.0.0.1:$port/$profile" endpoint)" || status=$?
	local ok=1 line
	[ "$status" -eq "$want_status" ] || ok=0
	for line in "$@"; do
		grep -qF -- "$line" <<<"$output" || ok=0
	done
	if [ "$ok" -eq 1 ]; then
		echo "ok   $name"
	else
		echo "FAIL $name: want exit $want_status with lines [$*], got exit $status"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	fi
}

expect check_live_passing_endpoint good 0 \
	"pass eth_chainId 0x7d" \
	"pass px_resolveAccount bound=false" \
	"pass px_getNetwork kernel.available=false reason=not_configured" \
	"pass lx_getAccount code=-32010 backend=public_core reason=not_configured" \
	"check-live: all checks passed"

expect check_live_wrong_chain_id wrong_chain 1 \
	'fail eth_chainId "0x1"' \
	"pass px_resolveAccount bound=false" \
	"check-live: 1 check(s) failed"

expect check_live_kernel_reported_reachable kernel_reachable 1 \
	"pass eth_chainId 0x7d" \
	'fail px_getNetwork {"available":true,"reason":"available"}' \
	"check-live: 1 check(s) failed"

expect check_live_kernel_method_answers_data kernel_data 1 \
	"fail lx_getAccount" \
	'"name":"account"' \
	"check-live: 1 check(s) failed"

expect check_live_resolve_read_error resolve_error 1 \
	"fail px_resolveAccount" \
	'"code":-32001' \
	"check-live: 1 check(s) failed"

status=0
output="$(CHECK_LIVE_ENDPOINT_BASE="http://127.0.0.1:1" CHECK_LIVE_TIMEOUT=2 "$checker" endpoint 2>&1)" || status=$?
if [ "$status" -eq 1 ] && grep -q '^fail eth_chainId transport ' <<<"$output" &&
	grep -q 'check-live: 4 check(s) failed' <<<"$output"; then
	echo "ok   check_live_transport_error"
else
	echo "FAIL check_live_transport_error: want exit 1 with transport failures, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

status=0
output="$(CHECK_LIVE_ENDPOINT_BASE="" "$checker" endpoint 2>&1)" || status=$?
if [ "$status" -eq 2 ] && grep -q 'CHECK_LIVE_ENDPOINT_BASE is required' <<<"$output" &&
	grep -q '^usage: ' <<<"$output"; then
	echo "ok   check_live_missing_base"
else
	echo "FAIL check_live_missing_base: want exit 2 with usage, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

status=0
output="$(CHECK_LIVE_ENDPOINT_BASE="http://127.0.0.1:$port/good" "$checker" nonexistent 2>&1)" || status=$?
if [ "$status" -eq 2 ] && grep -q '^usage: ' <<<"$output"; then
	echo "ok   check_live_unknown_mode"
else
	echo "FAIL check_live_unknown_mode: want exit 2 with usage, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

expect_attestors() {
	local name="$1" bases="$2" want_status="$3" output status=0
	shift 3
	output="$(CHECK_LIVE_ATTESTOR_BASES="$bases" CHECK_LIVE_TIMEOUT=5 "$checker" attestors 2>&1)" || status=$?
	local ok=1 line
	[ "$status" -eq "$want_status" ] || ok=0
	for line in "$@"; do
		grep -qF -- "$line" <<<"$output" || ok=0
	done
	if [ "$ok" -eq 1 ]; then
		echo "ok   $name"
	else
		echo "FAIL $name: want exit $want_status with lines [$*], got exit $status"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	fi
}

node_bases() {
	local out="" spec
	for spec in "$@"; do
		out="$out${out:+,}http://127.0.0.1:$port/attest_$spec"
	done
	printf '%s' "$out"
}

head="$(printf 'ab%.0s' $(seq 32))"

expect_attestors check_live_attestors_passing "$(node_bases ready/1 ready/2 ready/3 ready/4 ready/5)" 0 \
	"pass node 1 region=r1 shares=2 epoch=1 audit=7:$head peers=4/4 ready=true" \
	"pass node 3 region=r3 shares=2 epoch=1 audit=7:$head peers=4/4 ready=true" \
	"pass node 5 region=r5 shares=2 epoch=1 audit=7:$head peers=4/4 ready=true" \
	"pass quorum ready=5/5 need=3" \
	"check-live: all checks passed"

expect_attestors check_live_attestors_node_not_ready "$(node_bases ready/1 ready/2 ready/3 unready/4)" 1 \
	"pass node 1 region=r1" \
	"fail node 4 region=r4 shares=2 epoch=1 audit=7:$head peers=4/4 ready=false readiness_error=audit log broken" \
	"pass quorum ready=3/4 need=3" \
	"check-live: 1 check(s) failed"

expect_attestors check_live_attestors_peer_unreachable "$(node_bases ready/1 ready/2 ready/3 peerdown/4)" 1 \
	"pass node 3 region=r3" \
	"fail node 4 region=r4 shares=2 epoch=1 audit=7:$head peers=3/4 ready=true unreachable=5" \
	"pass quorum ready=4/4 need=3" \
	"check-live: 1 check(s) failed"

expect_attestors check_live_attestors_fewer_than_three "$(node_bases ready/1 ready/2)" 1 \
	"pass node 1 region=r1 shares=2 epoch=1 audit=7:$head peers=4/4 ready=true" \
	"pass node 2 region=r2 shares=2 epoch=1 audit=7:$head peers=4/4 ready=true" \
	"fail quorum ready=2/2 need=3" \
	"check-live: 1 check(s) failed"

expect_attestors check_live_attestors_quorum_lost "$(node_bases ready/1 unready/2 unready/3 ready/4)" 1 \
	"fail node 2 region=r2" \
	"fail node 3 region=r3" \
	"fail quorum ready=2/4 need=3" \
	"check-live: 3 check(s) failed"

expect_attestors check_live_attestors_transport_error "$(node_bases ready/1 ready/2 ready/3),http://127.0.0.1:1" 1 \
	"fail http://127.0.0.1:1 transport " \
	"pass quorum ready=3/4 need=3" \
	"check-live: 1 check(s) failed"

status=0
output="$(CHECK_LIVE_ATTESTOR_BASES="" "$checker" attestors 2>&1)" || status=$?
if [ "$status" -eq 2 ] && grep -q 'CHECK_LIVE_ATTESTOR_BASES is required' <<<"$output" &&
	grep -q '^usage: ' <<<"$output"; then
	echo "ok   check_live_attestors_missing_bases"
else
	echo "FAIL check_live_attestors_missing_bases: want exit 2 with usage, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

status=0
output="$(CHECK_LIVE_ATTESTOR_BASES="https://127.0.0.1:1" CHECK_LIVE_CLIENT_CERT="" "$checker" attestors 2>&1)" || status=$?
if [ "$status" -eq 2 ] && grep -q 'CHECK_LIVE_CLIENT_CERT is required for https attestor bases' <<<"$output"; then
	echo "ok   check_live_attestors_https_needs_identity"
else
	echo "FAIL check_live_attestors_https_needs_identity: want exit 2, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "check-live.test: $failures case(s) failed"
	exit 1
fi
echo "check-live.test: all cases passed"
