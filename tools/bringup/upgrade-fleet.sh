#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# sync-fleet lends the verified release fetch, its ssh helpers and the result
# printers; check-live, which it loads, lends the host map loader.
# shellcheck disable=SC1091
. "$here/sync-fleet.sh"

upgrade_usage() {
	cat <<'USAGE'
usage: tools/bringup/upgrade-fleet.sh [--stage-only|--watch-only] <height>

Carries every node unit of the fleet across a software upgrade. It fetches
paxd-<version>-linux-amd64 and SHA256SUMS of the tagged release from
PAXD_RELEASE_URL and checks the asset against SHA256SUMS through
tools/bringup/sync-fleet.sh's release path, then:

stage   copies the asset beside the running binary of every unit named in
        UPGRADE_UNITS as <binary>.new-<version> and checks its sha256 there.
watch   reads each unit's journal until it logs the upgrade halt line
        'UPGRADE "<name>" NEEDED at height: <height>', then once per unit
        keeps the running binary as <binary>.pre-<version>, moves the staged
        one into place, checks its sha256 and restarts the unit. A unit whose
        binary already hashes to the release is left running.

Without a flag it stages and then watches; --stage-only stops after the
stage, --watch-only skips it. Last it prints one status row per unit:
"<UPGRADE_UNITS[k]> <unit> staged|swapped|current|waiting|fail <step>".
A unit is named by its position in UPGRADE_UNITS and its unit name, never
by its destination.

Environment:
  BRINGUP_HOSTS_FILE     the private host map, required; it must also set
                         UPGRADE_UNITS, a list of
                         <destination>,<unit>[,<binary>] entries, one per
                         node unit; <binary> defaults to PAXD
  PAXD_RELEASE_URL       the base URL of the tagged release assets, required
  PAXD_RELEASE_DIR       where the fetched assets are kept
  PAXD_VERSION           the release version, default the version of
                         version.json
  UPGRADE_NAME           the upgrade plan name, default the upgrade of
                         version.json
  PAXD                   the default node binary, default /usr/local/bin/paxd
  UPGRADE_WATCH_TIMEOUT  seconds to watch for the halt, default 21600
  UPGRADE_POLL           seconds between journal reads, default 5
  UPGRADE_JOURNAL_LINES  journal lines read per unit, default 2000

Exits 0 when every unit is staged (--stage-only) or swapped or current, 1
when any is not, 2 on a usage error or a release asset that does not match
its SHA256SUMS.
USAGE
}

# halt_logged <upgrade name> <height>: status 0 when the log on stdin carries
# the upgrade halt line for that plan at exactly that height.
halt_logged() {
	grep -F -- "UPGRADE \"$1\" NEEDED at height" | grep -E -- "NEEDED at height:? ?$2([^0-9]|\$)" >/dev/null
}

# stage_unit <k>: copies the verified asset beside unit k's binary and checks
# it there, once per destination and binary.
stage_unit() {
	local k="$1" label="UPGRADE_UNITS[$1]" reply status=0
	local staged="${ubins[$k]}.new-$version" key="${udests[$k]} ${ubins[$k]}"
	if [ -n "${staged_on[$key]:-}" ]; then
		ustatus[k]=staged
		return 0
	fi
	scp -q -o BatchMode=yes -- "$bin" "${udests[$k]}:$staged" || status=$?
	if [ "$status" -ne 0 ]; then
		ustatus[k]="fail stage scp=$status"
		return 0
	fi
	reply="$(ssh_read "${udests[$k]}" "chmod 0755 $staged && sha256sum $staged")" || status=$?
	if [ "$status" -ne 0 ] || [ "${reply%% *}" != "$want_sha" ]; then
		ustatus[k]="fail verify sha256=${reply%% *} ssh=$status"
		return 0
	fi
	staged_on[$key]=1
	ustatus[k]=staged
	say "staged $label ${uunits[$k]} sha256=$want_sha"
}

# swap_unit <k>: moves the staged asset into place when it is still staged,
# checks the binary and restarts the unit.
swap_unit() {
	local k="$1" b="${ubins[$1]}" u="${uunits[$1]}" s="${ubins[$1]}.new-$version" reply status=0
	reply="$(remote "UPGRADE_UNITS[$k]" "${udests[$k]}" "set -e; if [ -f $s ]; then [ \"\$(sha256sum $s | cut -d' ' -f1)\" = $want_sha ]; cp -p $b $b.pre-$version; chmod 0755 $s; mv -f $s $b; fi; [ \"\$(sha256sum $b | cut -d' ' -f1)\" = $want_sha ]; systemctl restart $u; systemctl show $u -p ActiveState --value")" || status=$?
	if [ "$status" -ne 0 ]; then
		ustatus[k]="fail swap ssh=$status"
	else
		ustatus[k]="swapped unit=$reply"
		say "swapped UPGRADE_UNITS[$k] $u unit=$reply"
	fi
}

# watch_units: swaps every pending unit once its journal logs the halt, until
# none is pending or UPGRADE_WATCH_TIMEOUT has passed.
watch_units() {
	local k sha log pending start=$SECONDS
	for k in "${!udests[@]}"; do
		case "${ustatus[$k]}" in fail*) continue ;; esac
		sha="$(ssh_read "${udests[$k]}" "sha256sum ${ubins[$k]}")" || true
		if [ "${sha%% *}" = "$want_sha" ]; then
			ustatus[k]=current
		else
			ustatus[k]=waiting
		fi
	done
	while :; do
		pending=0
		for k in "${!udests[@]}"; do
			[ "${ustatus[$k]}" = waiting ] || continue
			log="$(ssh_read "${udests[$k]}" "journalctl -u ${uunits[$k]} -n $journal_lines --no-pager -o cat")" || true
			if halt_logged "$upgrade_name" "$height" <<<"$log"; then
				swap_unit "$k"
			else
				pending=$((pending + 1))
			fi
		done
		[ "$pending" -gt 0 ] && [ $((SECONDS - start)) -lt "$watch_timeout" ] || break
		sleep "$poll"
	done
}

# The dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

stage=1 watch=1
case "${1:-}" in
--stage-only)
	watch=0
	shift
	;;
--watch-only)
	stage=0
	shift
	;;
-h | --help)
	upgrade_usage
	exit 0
	;;
esac
if [ "$#" -ne 1 ] || ! [[ "$1" =~ ^[1-9][0-9]*$ ]]; then
	upgrade_usage >&2
	exit 2
fi
height="$1"
manifest="$here/../../version.json"
version="${PAXD_VERSION:-$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$manifest")}"
upgrade_name="${UPGRADE_NAME:-$(sed -n 's/^ *"upgrade": *"\([^"]*\)".*/\1/p' "$manifest")}"
watch_timeout="${UPGRADE_WATCH_TIMEOUT:-21600}"
poll="${UPGRADE_POLL:-5}"
journal_lines="${UPGRADE_JOURNAL_LINES:-2000}"
if [ -z "$version" ] || [ -z "$upgrade_name" ]; then
	echo "upgrade-fleet: no release version or upgrade name" >&2
	exit 2
fi
for tool in ssh scp timeout curl sha256sum; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "upgrade-fleet: $tool is required" >&2
		exit 2
	fi
done
load_hosts
if [ -z "${UPGRADE_UNITS:-}" ]; then
	echo "upgrade-fleet: BRINGUP_HOSTS_FILE lacks UPGRADE_UNITS" >&2
	exit 2
fi
release_asset "$version" || exit 2
echo "verified ${bin##*/} sha256=$want_sha upgrade=$upgrade_name height=$height"

declare -a udests uunits ubins ustatus
declare -A staged_on=()
read -r -a entries <<<"$UPGRADE_UNITS"
for k in "${!entries[@]}"; do
	IFS=, read -r udests[k] uunits[k] ubins[k] <<<"${entries[$k]}"
	ubins[k]="${ubins[$k]:-$paxd}"
	if [ -z "${udests[$k]}" ] || ! [[ "${uunits[$k]}" =~ ^[A-Za-z0-9@_.-]+$ ]]; then
		echo "upgrade-fleet: UPGRADE_UNITS[$k] is not <destination>,<unit>[,<binary>]" >&2
		exit 2
	fi
	ustatus[k]=pending
done

if [ "$stage" -eq 1 ]; then
	for k in "${!udests[@]}"; do
		stage_unit "$k"
	done
fi
[ "$watch" -eq 0 ] || watch_units

bad=0
printf '%-18s %-20s %s\n' unit name status
for k in "${!udests[@]}"; do
	printf '%-18s %-20s %s\n' "UPGRADE_UNITS[$k]" "${uunits[$k]}" "${ustatus[$k]}"
	case "${ustatus[$k]}" in
	swapped* | current) ;;
	staged) [ "$watch" -eq 0 ] || bad=$((bad + 1)) ;;
	*) bad=$((bad + 1)) ;;
	esac
done
if [ "$bad" -ne 0 ]; then
	echo "upgrade-fleet: $bad unit(s) not upgraded"
	exit 1
fi
echo "upgrade-fleet: all units passed"
