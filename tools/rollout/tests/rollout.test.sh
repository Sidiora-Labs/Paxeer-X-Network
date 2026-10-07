#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
rollout="$root/tools/rollout/rollout.sh"
ledger="$root/tools/rollout/ledger.sh"
manifest="$root/deploy/rollout.kvx"
sha=0123456789abcdef0123456789abcdef01234567
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export ROLLOUT_DRY_RUN=1
unset RAILWAY_API_TOKEN RAILWAY_PROJECT_ID ROLLOUT_MANIFEST

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

"$rollout" plan --sha "$sha" >"$tmp/plan"
mapfile -t want < <(sed -n 's/^\[service\.\(.*\)\]$/\1/p' "$manifest")
[ "${#want[@]}" -eq 27 ] || fail "manifest has ${#want[@]} services, want 27"
mapfile -t got < <(awk 'NR > 1 {print $2}' "$tmp/plan")
[ "${#got[@]}" -eq "${#want[@]}" ] || fail "plan lists ${#got[@]} of ${#want[@]} services"
for s in "${want[@]}"; do
	grep -qE "^ *[0-9]+ $s " "$tmp/plan" || fail "plan misses $s"
done
awk 'NR > 1 {print $1}' "$tmp/plan" | sort -n -c || fail "plan is not in wave order"

for s in identity router redis-router redis-internal internal-kms internal-journeys internal-payments \
	internal-approvals internal-programs webhooks-public webhooks-ingress dashboard dashboard-web interop \
	search-front ramp relay-archive-a relay-archive-b wallet-gateway wallet-postgres gas-station indexer \
	intent-ingester; do
	grep -qE "^ *[0-9]+ $s +railway +$s " "$tmp/plan" || fail "$s is not a railway target"
done
for s in kernel program-registry oracle-feeder explorer-index; do
	grep -qE "^ *[0-9]+ $s +box +$s ghcr\.io/sidiora-labs/[a-z0-9-]+:${sha:0:12} unit=[a-z0-9-]+\.service host=\\\$[A-Z_]+_HOST " "$tmp/plan" ||
		fail "$s is not a box target"
done
grep -qE " router +railway +router commit=$sha " "$tmp/plan" || fail "railway rows do not pin the commit"
grep -qE " redis-router +railway +redis-router skip stock-image=redis$" "$tmp/plan" || fail "stock redis is not skipped"
awk 'NR > 1 && $2 == "kernel" {k = $1} NR > 1 && $2 == "router" {r = $1} END {exit !(k < r)}' "$tmp/plan" ||
	fail "kernel does not precede router"

"$rollout" apply --sha "$sha" --only kernel,router >"$tmp/apply"
[ "$(awk 'NR > 1' "$tmp/apply" | wc -l)" -eq 2 ] || fail "--only did not narrow the plan"

expect_reject() {
	local name="$1"
	if ROLLOUT_MANIFEST="$tmp/$name.kvx" "$rollout" plan --sha "$sha" >/dev/null 2>"$tmp/$name.err"; then
		fail "malformed manifest $name was accepted"
	fi
	grep -q "malformed manifest" "$tmp/$name.err" || fail "$name rejected without a manifest error"
}
good='[service.a]
target = "railway"
image = "layerx-gateway"
railway_service = "a"
health = "/healthz"
order = 1'
printf '%s\n' "$good" | sed 's/^image = "layerx-gateway"$/image = layerx-gateway/' >"$tmp/unquoted.kvx"
printf '%s\n' "$good" | sed '/^health/d' >"$tmp/missing.kvx"
printf '%s\n' "$good" | sed 's/"railway"/"fly"/' >"$tmp/target.kvx"
printf '%s\nbox_unit = "a.service"\n' "$good" >"$tmp/extra.kvx"
printf '%s\n%s\n' "$good" "$good" >"$tmp/duplicate.kvx"
printf '%s\n' "$good" | sed 's/^order = 1$/order = "1"/' >"$tmp/order.kvx"
printf '%s\n' "$good" | sed 's#"/healthz"#"http://10.0.0.1/healthz"#' >"$tmp/health.kvx"
printf 'target = "railway"\n%s\n' "$good" >"$tmp/orphan.kvx"
printf '[deploy.a]\ntarget = "railway"\n' >"$tmp/section.kvx"
: >"$tmp/empty.kvx"
for name in unquoted missing target extra duplicate order health orphan section empty; do
	expect_reject "$name"
done
printf '%s\n' "$good" >"$tmp/good.kvx"
ROLLOUT_MANIFEST="$tmp/good.kvx" "$rollout" plan --sha "$sha" >/dev/null || fail "well-formed manifest rejected"

if "$rollout" plan --sha "${sha:0:12}" >/dev/null 2>&1; then fail "short sha accepted"; fi
if "$rollout" plan --sha "$sha" --only nope >/dev/null 2>&1; then fail "unknown --only accepted"; fi

export ROLLOUT_LEDGER_DIR="$tmp/ledger"
"$ledger" "$sha" router railway 2026-10-07T10:00:00Z pass /root/lx-ops/rollout/2026-10-07/x-router.log
"$ledger" "$sha" router railway 2026-10-07T10:05:00Z fail /root/lx-ops/rollout/2026-10-07/x-router.log
grep -qx "\[rollout.${sha:0:12}.router\]" "$tmp/ledger/2026-10-07.kvx" || fail "ledger record missing"
grep -qx "\[rollout.${sha:0:12}.router.2\]" "$tmp/ledger/2026-10-07.kvx" || fail "second ledger record missing"
if "$ledger" "$sha" router box 2026-10-07T10:00:00Z pass 10.1.2.3:/var/log/x >/dev/null 2>&1; then
	fail "ledger accepted evidence outside /root/lx-ops"
fi

echo "PASS rollout plan (${#want[@]} services), manifest rejection, ledger"
