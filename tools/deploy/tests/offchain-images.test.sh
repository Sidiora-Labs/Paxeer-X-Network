#!/usr/bin/env bash
set -euo pipefail

repo=$(cd "$(dirname "$0")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# a copy of the repository pieces the script reads, with a value for every NEXT_PUBLIC_* and WEB_* arg of Dockerfile.web
mkdir -p "$work/tools/deploy" "$work/deploy/launchpad/railway" "$work/services/platform/perps-data/deploy/nats"
cp "$repo/tools/deploy/offchain-images.sh" "$work/tools/deploy/"
cp "$repo"/deploy/launchpad/Dockerfile.{svc,native,web} "$work/deploy/launchpad/"
cp "$repo/services/platform/perps-data/deploy/Dockerfile" "$work/services/platform/perps-data/deploy/"
cp "$repo/services/platform/perps-data/deploy/nats/Dockerfile" "$work/services/platform/perps-data/deploy/nats/"
names=$(sed -n 's/^ARG \(\(NEXT_PUBLIC\|WEB\)_[A-Z0-9_]*\).*/\1/p' "$repo/deploy/launchpad/Dockerfile.web")
[ -n "$names" ] || fail "Dockerfile.web declares no NEXT_PUBLIC_* args"
grep -qx WEB_API_UPSTREAM <<<"$names" || fail "Dockerfile.web declares no WEB_API_UPSTREAM arg"
jq --arg names "$names" '.web.buildArgs = ([$names | splits("\n") | {key: ., value: "https://\(. | ascii_downcase).example"}] | from_entries)' \
	"$repo/deploy/launchpad/railway/variables.json" > "$work/deploy/launchpad/railway/variables.json"
# the perps frontend Dockerfile (until it lands, one ARG per name of the committed build args file) and the committed
# values with the gateway origin filled in
mkdir -p "$work/apps/perps/deploy"
perps_src="$repo/apps/perps/deploy/build-args.production.env"
perps_env="$work/apps/perps/deploy/build-args.production.env"
if [ -f "$repo/apps/perps/Dockerfile" ]; then
	cp "$repo/apps/perps/Dockerfile" "$work/apps/perps/"
else
	sed -n 's/^\(NEXT_PUBLIC_[A-Z0-9_]*\)=.*/ARG \1=/p' "$perps_src" > "$work/apps/perps/Dockerfile"
fi
sed 's|^NEXT_PUBLIC_PERPS_GATEWAY_URL=$|NEXT_PUBLIC_PERPS_GATEWAY_URL=https://gateway.example|' "$perps_src" > "$perps_env"
perps_pristine=$(cat "$perps_env")
script="$work/tools/deploy/offchain-images.sh"
manifest="$work/deploy/launchpad/railway/variables.json"
pristine=$(cat "$manifest")

sha=0123456789ab
run() { env -u REVIEWED_BUILD_IMAGE -u REVIEWED_RUNTIME_IMAGE -u PERPS_BUILD_IMAGE -u PERPS_RUNTIME_IMAGE BUILD_SHA=$sha "$@"; }
args_of() { jq -r --arg n "$2" '.[] | select(.name == $n) | .build_args' <<<"$1"; }

run bash "$script" --check || fail "--check must pass on a complete tree"

# unset base images are left out so the Dockerfile defaults hold
m=$(run bash "$script" --matrix)
jq -e 'length > 0 and all(.[]; keys == ["build_args","context","dockerfile","name","target"])' <<<"$m" >/dev/null || fail "matrix shape changed"
# every launchpad image is named launchpad-<service>; no kindlelaunch- image name remains
lp_names=$(jq -r '.[] | select(.name | startswith("launchpad-")) | .name' <<<"$m")
[ "$lp_names" = "$(printf '%s\n' launchpad-core-api launchpad-core-indexer launchpad-core-pnl-tracker \
	launchpad-core-ranking-algo launchpad-core-stats-workers launchpad-core-trading-charts launchpad-media-gateway \
	launchpad-media-livestream launchpad-media-social launchpad-media-user launchpad-media-metadata launchpad-web)" ] ||
	fail "launchpad image names: $lp_names"
! jq -r '.[].name' <<<"$m" | grep -q 'kindlelaunch-' || fail "a kindlelaunch- image name remains in the matrix"
! grep -q 'kindlelaunch-' "$repo/tools/deploy/offchain-images.sh" "$repo/deploy/rollout-launchpad.kvx" ||
	fail "a kindlelaunch- image name remains in the image table or the rollout manifest"
[ "$(sed -n 's/^image = "\(.*\)"$/\1/p' "$repo/deploy/rollout-launchpad.kvx" | sort -u)" = \
	"$(jq -r '.[] | select(.name | startswith("launchpad-")) | .name' <<<"$m" | sort -u)" ] ||
	fail "the rollout manifest images differ from the launchpad matrix rows"
! jq -r '.[].build_args' <<<"$m" | grep -E '^[A-Z_]+=$' || fail "matrix carries an empty build arg"
! jq -r '.[].build_args' <<<"$m" | grep -E '^(BUILD|RUNTIME)_IMAGE=' || fail "unset base images must be omitted"
[ "$(args_of "$m" launchpad-core-api)" = $'SERVICE=api\nBIN=apid' ] || fail "api args: $(args_of "$m" launchpad-core-api)"

# the repository variable names reach the launchpad and perps rows
m=$(run env REVIEWED_BUILD_IMAGE=rb@sha256:1 REVIEWED_RUNTIME_IMAGE=rr@sha256:2 PERPS_BUILD_IMAGE=pb@sha256:3 bash "$script" --matrix)
[ "$(args_of "$m" launchpad-media-metadata)" = $'SERVICE=metadata\nBIN=metadatad\nBUILD_IMAGE=rb@sha256:1\nRUNTIME_IMAGE=rr@sha256:2' ] || fail "metadata args: $(args_of "$m" launchpad-media-metadata)"
[ "$(args_of "$m" perps-gateway)" = $'SERVICE=perps-gateway\nBUILD_IMAGE=pb@sha256:3' ] || fail "perps args: $(args_of "$m" perps-gateway)"

# the web row carries every NEXT_PUBLIC_* arg with its manifest value, and the build sha as the Sentry release
web=$(args_of "$m" launchpad-web)
for n in $names; do
	[ "$n" = NEXT_PUBLIC_SENTRY_RELEASE ] && continue
	grep -qx "$n=https://${n,,}.example" <<<"$web" || fail "web row lacks $n"
done
grep -qx "NEXT_PUBLIC_SENTRY_RELEASE=$sha" <<<"$web" || fail "web row Sentry release is not the build sha"
[ "$(wc -l <<<"$web")" -eq "$(wc -w <<<"$names")" ] || fail "web row has extra args"
grep -qx "WEB_API_UPSTREAM=https://web_api_upstream.example" <<<"$web" || fail "web row lacks the WEB_API_UPSTREAM build arg"

# a WEB_* arg without a manifest value fails --check and is named, like a NEXT_PUBLIC_* one
jq 'del(.web.buildArgs.WEB_API_UPSTREAM)' <<<"$pristine" > "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing WEB_API_UPSTREAM"; fi
grep -q 'launchpad-web: .*WEB_API_UPSTREAM' <<<"$out" || fail "--check must name WEB_API_UPSTREAM: $out"
printf '%s\n' "$pristine" > "$manifest"

# a value with a space is refused with exit 2
first=$(head -n1 <<<"$names")
jq --arg n "$first" '.web.buildArgs[$n] = "https://a b"' <<<"$pristine" > "$manifest"
set +e
run bash "$script" --matrix >/dev/null 2>&1; rc=$?
set -e
[ "$rc" -eq 2 ] || fail "a value with a space must exit 2, got $rc"

# a NEXT_PUBLIC_* arg without a manifest value fails --check and is named
last=$(grep -vE "SOCIAL_API_URL|USER_API_URL|ANALYTICS_KEY|QUALIFICATION_AUTHORITY_PROFILE|SENTRY_RELEASE" <<<"$names" | tail -n1)
jq --arg a "$first" --arg b "$last" 'del(.web.buildArgs[$a], .web.buildArgs[$b])' <<<"$pristine" > "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing value"; fi
grep -q "$first" <<<"$out" && grep -q "$last" <<<"$out" || fail "--check must name the missing args: $out"

# the committed web values: the three same-origin origins are set, the four dead or unset-is-production names may be
# empty, the Sentry release is the sha passed, and a backend host without a value still fails --check
real="$repo/deploy/launchpad/railway/variables.json"
for o in NEXT_PUBLIC_PRIVATE_API_ORIGIN=https://sidiora.fun NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN=wss://sidiora.fun \
	NEXT_PUBLIC_STREAM_ORIGIN=wss://sidiora.fun; do
	[ "$(jq -r --arg n "${o%%=*}" '.web.buildArgs[$n] // empty' "$real")" = "${o#*=}" ] || fail "committed manifest lacks $o"
done
may="NEXT_PUBLIC_SOCIAL_API_URL NEXT_PUBLIC_USER_API_URL NEXT_PUBLIC_ANALYTICS_KEY NEXT_PUBLIC_PRIVATE_QUALIFICATION_AUTHORITY_PROFILE"
jq --arg may "$may" '.web.buildArgs |= with_entries(select(.key as $k | $may | split(" ") | index($k) | not)) | del(.web.buildArgs.NEXT_PUBLIC_SENTRY_RELEASE)' \
	<<<"$pristine" > "$manifest"
run bash "$script" --check || fail "--check must allow the four may-be-empty web names and take the release from the sha"
[ "$(args_of "$(run bash "$script" --matrix)" launchpad-web | grep '^NEXT_PUBLIC_SENTRY_RELEASE=')" = "NEXT_PUBLIC_SENTRY_RELEASE=$sha" ] ||
	fail "the Sentry release is not the sha passed"
jq '.web.buildArgs.NEXT_PUBLIC_DATA_API_URL = ""' <<<"$(cat "$manifest")" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on an empty data API URL"; fi
grep -q 'launchpad-web: .*NEXT_PUBLIC_DATA_API_URL' <<<"$out" || fail "--check must name the data API URL: $out"
! grep -qE 'SOCIAL_API_URL|USER_API_URL|ANALYTICS_KEY|QUALIFICATION_AUTHORITY_PROFILE|SENTRY_RELEASE' <<<"$out" ||
	fail "--check named an allowed web name: $out"
printf '%s\n' "$pristine" > "$manifest"

# --plan on the committed web values, which leave web args without a value: --check still fails, the web row alone
# is unbuilt with an error annotation naming the missing arg, and every other row is planned exactly as --matrix has it
full=$(run bash "$script" --matrix)
jq 'del(.web.buildArgs.NEXT_PUBLIC_DATA_API_URL)' "$real" > "$manifest"
if run bash "$script" --check 2>/dev/null; then fail "--check must fail on the committed web values"; fi
run bash "$script" --plan > "$work/plan.out" 2> "$work/plan.err" || fail "--plan must pass when a row passes: $(cat "$work/plan.err")"
[ "$(grep -c . "$work/plan.out")" -eq 2 ] || fail "--plan must print exactly images= and unbuilt=: $(cat "$work/plan.out")"
[ "$(sed -n 's/^unbuilt=//p' "$work/plan.out")" = '["launchpad-web"]' ] || fail "unbuilt: $(cat "$work/plan.out")"
grep -q '^::error title=launchpad-web::.*NEXT_PUBLIC_DATA_API_URL' "$work/plan.err" || fail "--plan must annotate the web row: $(cat "$work/plan.err")"
[ "$(grep -c '^::error' "$work/plan.err")" -eq 1 ] || fail "--plan must annotate only the web row: $(cat "$work/plan.err")"
planned=$(sed -n 's/^images=//p' "$work/plan.out")
[ "$planned" = "$(jq -c 'map(select(.name != "launchpad-web"))' <<<"$full")" ] || fail "--plan images differ from --matrix: $planned"
# a perps value with whitespace leaves only perps-web unbuilt as well, and no passing row exits 1
sed -i 's|^NEXT_PUBLIC_LOG_LEVEL=.*|NEXT_PUBLIC_LOG_LEVEL=warn x|' "$perps_env"
out=$(run bash "$script" --plan 2>/dev/null) || fail "--plan must pass with a perps value with whitespace"
[ "$(sed -n 's/^unbuilt=//p' <<<"$out")" = '["launchpad-web","perps-web"]' ] || fail "unbuilt: $out"
printf '%s\n' "$perps_pristine" > "$perps_env"
mv "$work/deploy" "$work/deploy.away" && mv "$work/services" "$work/services.away"
sed -i 's|^NEXT_PUBLIC_PERPS_GATEWAY_URL=.*|NEXT_PUBLIC_PERPS_GATEWAY_URL=|' "$perps_env"
if out=$(run bash "$script" --plan 2>/dev/null); then fail "--plan must exit 1 when no row passes: $out"; fi
mv "$work/deploy.away" "$work/deploy" && mv "$work/services.away" "$work/services"
printf '%s\n' "$perps_pristine" > "$perps_env"
printf '%s\n' "$pristine" > "$manifest"

# the perps-web row carries exactly the non-empty values of the build args file
want=$(grep -E '^NEXT_PUBLIC_[A-Z0-9_]+=.' "$perps_env" | sort)
[ "$(args_of "$(run bash "$script" --matrix)" perps-web | sort)" = "$want" ] || fail "perps-web args differ from the build args file"
grep -qx 'NEXT_PUBLIC_PAXSCAN_BASE_URL=https://paxscan.io' <<<"$want" || fail "perps-web lacks the explorer URL"

# an empty value without a code fallback fails --check and is named; an empty fallback value does not
sed -i 's|^NEXT_PUBLIC_PERPS_GATEWAY_URL=.*|NEXT_PUBLIC_PERPS_GATEWAY_URL=|' "$perps_env"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on an empty perps gateway"; fi
grep -q 'perps-web: .*NEXT_PUBLIC_PERPS_GATEWAY_URL' <<<"$out" || fail "--check must name the perps gateway: $out"
! grep -q 'NEXT_PUBLIC_PAXSCAN_API_URL' <<<"$out" || fail "--check must allow the empty paxscan API URL: $out"

# a perps value with a space is refused with exit 2
printf '%s\n' "$perps_pristine" | sed 's|^NEXT_PUBLIC_LOG_LEVEL=.*|NEXT_PUBLIC_LOG_LEVEL=warn x|' > "$perps_env"
set +e
run bash "$script" --matrix >/dev/null 2>&1; rc=$?
set -e
[ "$rc" -eq 2 ] || fail "a perps value with a space must exit 2, got $rc"
printf '%s\n' "$perps_pristine" > "$perps_env"

# a missing Dockerfile of a required row fails --check
rm "$work/deploy/launchpad/Dockerfile.native"
if out=$(run bash "$script" --check 2>&1); then fail "--check must fail on a missing dockerfile"; fi
grep -q 'launchpad-media-metadata' <<<"$out" || fail "--check must name the row: $out"


# the workflow: the build matrix does not fail fast, and the digests job runs after any image row fails as long as the
# plan passed and the run was not cancelled
wf="$repo/.github/workflows/offchain-images.yml"
job() { python3 -c 'import json,sys,yaml; print(json.dumps(yaml.safe_load(open(sys.argv[1]))["jobs"][sys.argv[2]]))' "$wf" "$1"; }
[ "$(job images | jq '.strategy["fail-fast"]')" = false ] || fail "the images matrix must keep fail-fast: false"
[ "$(job digests | jq -r '.if')" = "\${{ !cancelled() && needs.plan.result == 'success' }}" ] ||
	fail "the digests job must run unless cancelled once the plan passed: $(job digests | jq -r '.if')"
job digests | jq -r '.steps[] | select(.id == "merge") | .run' > "$work/merge.sh"
[ -s "$work/merge.sh" ] || fail "the digests job has no merge step"

# the merge step on a fixture: one row pushed, one planned row without a digest, one unbuilt row
merge() {
	local d="$work/merge.$1"; shift
	rm -rf "$d" && mkdir -p "$d/digests" && : > "$d/out" && : > "$d/summary"
	local f; for f in "$@"; do printf '%s=sha256:%064d\n' "$f" 0 > "$d/digests/$f"; done
	(cd "$d" && IMAGES='[{"name":"perps-gateway"},{"name":"launchpad-core-api"}]' UNBUILT='["launchpad-web"]' \
		GITHUB_OUTPUT="$d/out" GITHUB_STEP_SUMMARY="$d/summary" bash "$work/merge.sh" >"$d/log" 2>&1)
}
merge partial perps-gateway || fail "the merge must pass when one row pushed: $(cat "$work/merge.partial/log")"
[ "$(jq -c 'keys' "$work/merge.partial/digests.json")" = '["perps-gateway"]' ] || fail "digests.json: $(cat "$work/merge.partial/digests.json")"
grep -qx 'failed=\["launchpad-core-api"\]' "$work/merge.partial/out" || fail "failed output: $(cat "$work/merge.partial/out")"
grep -qx -e '- launchpad-core-api was planned but its build or push failed: .*' "$work/merge.partial/summary" ||
	fail "the summary must list the failed row: $(cat "$work/merge.partial/summary")"
grep -qx -e '- launchpad-web was not built: .*' "$work/merge.partial/summary" || fail "the summary must list the unbuilt row"
! grep -q 'perps-gateway' "$work/merge.partial/summary" || fail "the summary lists the pushed row"
# nothing pushed fails the job; an unbuilt row with a digest fails it as well
if merge none; then fail "the merge must fail when digests.json is empty"; fi
grep -q 'digests.json is empty' "$work/merge.none/log" || fail "empty merge log: $(cat "$work/merge.none/log")"
grep -c 'build or push failed' "$work/merge.none/summary" | grep -qx 2 || fail "the empty merge must still list both failed rows"
if merge unbuilt perps-gateway launchpad-web; then fail "the merge must fail when an unbuilt row has a digest"; fi
grep -q 'an unbuilt row has a digest' "$work/merge.unbuilt/log" || fail "unbuilt merge log: $(cat "$work/merge.unbuilt/log")"
echo "PASS offchain-images"
