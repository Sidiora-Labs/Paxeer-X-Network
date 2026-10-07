#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"

if [ "$#" -ne 6 ]; then
	echo "usage: ledger.sh <sha40> <service> <railway|box> <started_at> <pass|fail|blocked> <log path>" >&2
	exit 2
fi
sha="$1" svc="$2" target="$3" started="$4" outcome="$5" evidence="$6"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || { echo "bad revision $sha" >&2; exit 2; }
[[ "$svc" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "bad service $svc" >&2; exit 2; }
[[ "$target" =~ ^(railway|box)$ ]] || { echo "bad target $target" >&2; exit 2; }
[[ "$started" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || { echo "bad started_at $started" >&2; exit 2; }
[[ "$outcome" =~ ^(pass|fail|blocked)$ ]] || { echo "bad outcome $outcome" >&2; exit 2; }
[[ "$evidence" =~ ^/root/lx-ops/[A-Za-z0-9._/-]+$ && "$evidence" != *..* ]] || { echo "evidence must be a log path under /root/lx-ops" >&2; exit 2; }

dir="${ROLLOUT_LEDGER_DIR:-$root/deploy/ledger}"
file="$dir/${started%%T*}.kvx"
mkdir -p "$dir"
section="rollout.${sha:0:12}.$svc"
if [ -f "$file" ] && grep -qF "[$section]" "$file"; then
	n=$(grep -cE "^\[$section(\.[0-9]+)?\]$" "$file")
	section="$section.$((n + 1))"
fi
{
	[ -s "$file" ] && echo
	printf '[%s]\nrevision = "%s"\ntarget = "%s"\nstarted_at = "%s"\noutcome = "%s"\nevidence = "%s"\n' \
		"$section" "$sha" "$target" "$started" "$outcome" "$evidence"
} >>"$file"
