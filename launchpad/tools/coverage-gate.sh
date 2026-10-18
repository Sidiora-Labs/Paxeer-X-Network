#!/usr/bin/env bash
# coverage-gate.sh — enforce the KindleLaunch coverage gate (SECTION 17).
#
# Reads per-module coverage profiles from .cover/<module-with-slashes-as-_>.out
# (produced by `make cover`) and fails if any module is below its threshold:
#   - money / correctness-critical modules : 90%   (env MONEY="shared protocol ...")
#   - all other modules                    : 85%
#
# Modules with no measurable statements yet (empty scaffold) are reported and
# skipped — the gate bites as soon as a module ships code.
set -euo pipefail

COVER_DIR="${COVER_DIR:-.cover}"
DEFAULT_MIN="${DEFAULT_MIN:-85.0}"
MONEY_MIN="${MONEY_MIN:-90.0}"
MONEY="${MONEY:-}"

if [ ! -d "$COVER_DIR" ]; then
	echo "coverage-gate: no $COVER_DIR directory (run 'make cover' first)" >&2
	exit 1
fi

fail=0
shopt -s nullglob
profiles=("$COVER_DIR"/*.out)
if [ ${#profiles[@]} -eq 0 ]; then
	echo "coverage-gate: no coverage profiles found in $COVER_DIR" >&2
	exit 1
fi

is_money() {
	local mod="$1" m
	for m in $MONEY; do [ "$m" = "$mod" ] && return 0; done
	return 1
}

# filter_generated <profile> <module-dir>
# Echoes the path to a copy of <profile> with all lines for generated files
# (those whose source carries the canonical "// Code generated ... DO NOT EDIT."
# marker, e.g. abigen bindings, sqlc output) removed, so machine-written code is
# never counted toward the coverage gate. If nothing is filtered, the original
# profile path is echoed unchanged. Caller removes the temp file when it differs.
filter_generated() {
	local prof="$1" mod="$2"
	[ -f "$mod/go.mod" ] || { echo "$prof"; return; }

	local modpath
	modpath="$(awk '/^module /{print $2; exit}' "$mod/go.mod")"

	local patt
	patt="$(mktemp)"
	local f rel
	while IFS= read -r f; do
		rel="${f#"$mod"/}"
		printf '%s/%s:\n' "$modpath" "$rel"
	done < <(grep -rlE '^// Code generated .* DO NOT EDIT\.$' "$mod" --include='*.go' 2>/dev/null || true) >"$patt"

	if [ ! -s "$patt" ]; then
		rm -f "$patt"
		echo "$prof"
		return
	fi

	local out
	out="$(mktemp)"
	grep -vF -f "$patt" "$prof" >"$out" || true
	rm -f "$patt"
	echo "$out"
}

printf '%-26s %8s %8s   %s\n' "MODULE" "COVER" "MIN" "STATUS"
printf '%-26s %8s %8s   %s\n' "------" "-----" "---" "------"

for prof in "${profiles[@]}"; do
	base="$(basename "$prof" .out)"
	# Reverse the slash->underscore mapping (each module path has at most one slash).
	mod="${base/_//}"

	# A profile with only the "mode:" header has no statements (empty module).
	if [ "$(wc -l <"$prof")" -le 1 ]; then
		printf '%-26s %8s %8s   %s\n' "$mod" "n/a" "-" "SKIP (no code yet)"
		continue
	fi

	# Drop generated files (abigen/sqlc) before measuring — only hand-written
	# code counts toward the gate.
	fprof="$(filter_generated "$prof" "$mod")"
	pct="$(go tool cover -func="$fprof" | awk '/^total:/ {gsub(/%/,"",$3); print $3}')"
	[ "$fprof" != "$prof" ] && rm -f "$fprof"
	if [ -z "$pct" ]; then
		printf '%-26s %8s %8s   %s\n' "$mod" "n/a" "-" "SKIP (no statements)"
		continue
	fi

	if is_money "$mod"; then min="$MONEY_MIN"; else min="$DEFAULT_MIN"; fi

	if awk -v p="$pct" -v m="$min" 'BEGIN{exit !(p+0 >= m+0)}'; then
		printf '%-26s %7s%% %7s%%   %s\n' "$mod" "$pct" "$min" "PASS"
	else
		printf '%-26s %7s%% %7s%%   %s\n' "$mod" "$pct" "$min" "FAIL"
		fail=1
	fi
done

if [ "$fail" -ne 0 ]; then
	echo "" >&2
	echo "coverage-gate: FAILED — one or more modules below threshold" >&2
	exit 1
fi
echo ""
echo "coverage-gate: PASSED"
