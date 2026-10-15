#!/usr/bin/env bash
set -euo pipefail

# Runs deploy.sh from a checkout against an empty artifacts root: the asset
# preflight must accept the committed logo and stop only at the missing release,
# before any host change. Also checks every logo path the installer reads exists.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work:?}"' EXIT

failures=0
check() {
	if (eval "$2"); then
		echo "ok   $1"
	else
		echo "FAIL $1"
		failures=$((failures + 1))
	fi
}

[ "$(id -u)" -eq 0 ] || { echo "FAIL deploy.sh preflight requires root"; exit 1; }

mkdir "$work/artifacts"
set +e
HPX_SOURCE_REVISION=0123456789abcdef0123456789abcdef01234567 HPX_ARTIFACTS_ROOT="$work/artifacts" \
	bash "$here/deploy.sh" >"$work/out" 2>"$work/err"
status=$?
set -e

check preflight_exits_nonzero '[ "$status" -eq 1 ]'
check logo_preflight_passes '! grep -qF "missing Paxeer logo" "$work/err"'
check stops_at_missing_release 'grep -qF "publish an HPX release before deployment: $work/artifacts/current" "$work/err"'
check logo_references_exist 'refs=$(grep -oE "\"\\\$(SCRIPT_DIR|PAXEER_ROOT)/[^\"]*PaxeerLogo\.png\"" "$here/deploy.sh" | tr -d "\"" | sed "s#^\\\$SCRIPT_DIR#$here#; s#^\\\$PAXEER_ROOT#$root#") &&
	[ "$(printf "%s\n" "$refs" | wc -l)" -eq 2 ] && while read -r ref; do [ -f "$ref" ] || exit 1; done <<<"$refs"'

[ "$failures" -eq 0 ] || { echo "$failures failed"; exit 1; }
echo "all passed"
