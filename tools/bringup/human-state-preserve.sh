#!/usr/bin/env bash
set -euo pipefail

# The Fly helpers and the app lookup are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/human-state-preserve.sh export <dir> | import <dir> | verify <dir>

Carries the retained state and its matching cryptographic material from the
writable root of the volumeless machine of the kernel app
(human/wallet/deploy/human.toml) onto the kernel volume, so nothing is
regenerated in its place.

What is carried, from the old machine's root to the volume at /data:
  /var/lib/layerx/human  -> /data/human-state/components  the human service's
                            store, custody and auth-index roots (the pod mounts
                            human-state/components there)
  /data/human-state      -> /data/human-state             components, identity,
                            security, movement, agent, authority, kms
  /data/layerx           -> /data/layerx                  keys, genesis, node,
                            guarantor-*, settlement
  /data/tls              -> /data/tls
  /run/human-material    -> /data/layerx/keys/human-material  the material the
                            role entrypoints stage from
Not carried: /run/layerx (runtime clock, sockets), /run/human-private and
/run/authority-private (per-start copies of the material above), and every
socket. On import human-state/{components,identity,security,movement} become
4020:4020 0700, agent and authority 4021:4020 0700, kms 4026:4020 0700,
human-state root:4020 0750 and keys/human-material root:4020 0750.

export <dir>  on the only started machine, the old one (refused when /data is a
              mount): stops (SIGSTOP) every layerx-human-service and
              layerx-runtime-clock process so nothing writes, refuses when a
              state root in their environment lies outside
              /var/lib/layerx/human, writes <dir>/manifest.sha256 with the
              sha256 of every carried file under its /data path, streams
              <dir>/state.tar through flyctl ssh console and checks the tar
              against the manifest. Refuses when <dir> already holds an export
              or nothing is carried.
import <dir>  on the only started machine, the new one with /data, before the
              kernel init first runs: refuses when any carried file already
              exists there, unpacks the tar under /data, sets the owners above,
              then runs verify.
verify <dir>  on the only started machine: every manifest file has its sha256
              under /data and every human-state directory has its owner.

Prints file counts and pass lines, refusals on stderr, never a key or a state
byte. CHECK_LIVE_TIMEOUT bounds each flyctl call; set it to cover the transfer.
Exits 0 on success, 1 on a refusal or mismatch, 2 on a usage error.
EOF
}

toml=human/wallet/deploy/human.toml

# shellcheck disable=SC2016
quiesce_cmd='mountpoint -q /data/ && exit 3
s=/var/lib/layerx/human
for d in /proc/[0-9]*; do
	case "$(readlink "$d/exe" 2>/dev/null)" in
	/usr/local/bin/layerx-human-service | /usr/local/bin/layerx-runtime-clock)
		for r in $(tr "\000" "\n" <"$d/environ" | sed -n "s/^LAYERX_HUMAN_\(STORE\|CUSTODY\|AUTH_INDEX\)_ROOT=//p"); do
			case "$r/" in "$s"/*) ;; *) exit 4 ;; esac
		done
		;;
	esac
done
n=0
for d in /proc/[0-9]*; do
	case "$(readlink "$d/exe" 2>/dev/null)" in
	/usr/local/bin/layerx-human-service | /usr/local/bin/layerx-runtime-clock)
		kill -STOP "${d#/proc/}" && n=$((n + 1))
		;;
	esac
done
echo "@@quiesced $n"
sums() { find "$@" -type f -print0 | xargs -0 -r sha256sum; }
! test -d "$s" || (cd /var/lib/layerx/ && sums human) | sed "s,  human/,  human-state/components/,"
for m in human-state layerx tls; do ! test -d "/data/$m" || (cd /data/ && sums "$m"); done
! test -d /run/human-material || (cd /run/human-material/.. && sums human-material) | sed "s,  human-material/,  layerx/keys/human-material/,"'

# shellcheck disable=SC2016
tar_cmd='set --
! test -d /var/lib/layerx/human || set -- "$@" -C /var/lib/layerx/ human
for m in human-state layerx tls; do ! test -d "/data/$m" || set -- "$@" -C /data/ "$m"; done
! test -d /run/human-material || set -- "$@" -C /run/human-material/.. human-material
tar -cf - --transform "s,^human\(/\|$\),human-state/components\1," --transform "s,^human-material,layerx/keys/human-material," "$@"'

# shellcheck disable=SC2016
absent_cmd='test -d /data/ || exit 3
cd /data/ && while read -r h f; do ! test -e "$f" || exit 4; done'

# shellcheck disable=SC2016
import_cmd='cd /data/ && tar -xpf - || exit 1
if test -d /data/human-state; then
	chown 0:4020 /data/human-state && chmod 0750 /data/human-state
	own() { u=$1; shift; for r; do ! test -d "$r" || { chown -R "$u:4020" "$r" && chmod 0700 "$r"; } || exit 1; done; }
	cd /data/human-state && own 4020 components identity security movement && own 4021 agent authority && own 4026 kms
fi
! test -d /data/layerx/keys/human-material || { chown -R 0:4020 /data/layerx/keys/human-material && chmod 0750 /data/layerx/keys/human-material; }'

# shellcheck disable=SC2016
verify_cmd='cd /data/ || exit 3
sha256sum --quiet -c - >/dev/null 2>&1 || exit 5
cd /data/human-state 2>/dev/null || exit 0
own() { u=$1; shift; for r; do ! test -d "$r" || test -z "$(find "$r" ! -user "$u" -print -o ! -group 4020 -print | head -n 1)" || exit 6; done; }
own 4020 components identity security movement
own 4021 agent authority
own 4026 kms'

refuse() {
	echo "human-state-preserve: $*" >&2
	exit 1
}

# check_tar <dir>: the tar of <dir> unpacks locally to exactly the manifest's
# digests.
check_tar() {
	local scratch rc=0
	scratch="$(mktemp -d)"
	tar -C "$scratch" -xf "$1/state.tar" && (cd "$scratch" && sha256sum --quiet -c "$1/manifest.sha256" >/dev/null 2>&1) || rc=1
	rm -rf "$scratch"
	return "$rc"
}

do_export() {
	local dir=$1 app out rc=0 files stopped
	if [ -e "$dir/manifest.sha256" ] || [ -e "$dir/state.tar" ]; then
		refuse "$dir already holds an export"
	fi
	app="$(fly_app "$toml")" || refuse "$toml names no app"
	mkdir -p "$dir"
	chmod 0700 "$dir"
	out="$(fly_ssh "$app" - "$quiesce_cmd" </dev/null)" || rc=$?
	case "$rc" in
	0) ;;
	3) refuse "the started machine of $app has a volume at /data; export runs on the old machine" ;;
	4) refuse "a human state root on $app lies outside /var/lib/layerx/human" ;;
	*) refuse "quiesce on $app failed with status $rc" ;;
	esac
	stopped="$(sed -n '1s/^@@quiesced \([0-9][0-9]*\)$/\1/p' <<<"$out")"
	[ -n "$stopped" ] || refuse "quiesce on $app gave no process count"
	echo "pass quiesce app=$app stopped=$stopped"
	sed '1d' <<<"$out" >"$dir/manifest.sha256"
	chmod 0600 "$dir/manifest.sha256"
	files="$(grep -c . "$dir/manifest.sha256" || true)"
	if [ "$files" -eq 0 ]; then
		rm -f "$dir/manifest.sha256"
		refuse "no carried path on $app holds a file; nothing to preserve"
	fi
	(umask 077 && fly_ssh "$app" - "$tar_cmd" </dev/null >"$dir/state.tar") || {
		rm -f "$dir/state.tar"
		refuse "streaming the state of $app failed"
	}
	check_tar "$dir" || refuse "$dir/state.tar does not match $dir/manifest.sha256"
	echo "pass export app=$app files=$files manifest=match"
}

do_verify() {
	local dir=$1 app rc=0 files
	app="$(fly_app "$toml")" || refuse "$toml names no app"
	files="$(grep -c . "$dir/manifest.sha256")"
	fly_ssh "$app" - "$verify_cmd" <"$dir/manifest.sha256" >/dev/null || rc=$?
	case "$rc" in
	0) echo "pass verify app=$app files=$files sha256=match owners=match" ;;
	3) refuse "/data is absent on $app" ;;
	5) refuse "a file on $app differs from $dir/manifest.sha256 or is missing" ;;
	6) refuse "a human-state directory on $app has another owner than the init expects" ;;
	*) refuse "verify on $app failed with status $rc" ;;
	esac
}

do_import() {
	local dir=$1 app rc=0
	check_tar "$dir" || refuse "$dir/state.tar does not match $dir/manifest.sha256"
	app="$(fly_app "$toml")" || refuse "$toml names no app"
	fly_ssh "$app" - "$absent_cmd" <"$dir/manifest.sha256" >/dev/null || rc=$?
	case "$rc" in
	0) ;;
	3) refuse "the started machine of $app has no /data; import runs on the machine with the kernel volume" ;;
	4) refuse "a carried file already exists under /data on $app; nothing was overwritten" ;;
	*) refuse "the pre-import check on $app failed with status $rc" ;;
	esac
	fly_ssh "$app" - "$import_cmd" <"$dir/state.tar" >/dev/null || refuse "import on $app failed with status $?"
	echo "pass import app=$app files=$(grep -c . "$dir/manifest.sha256")"
	do_verify "$dir"
}

if [ "$#" -ne 2 ]; then
	usage >&2
	exit 2
fi
case "$1" in
export | import | verify) ;;
*)
	usage >&2
	exit 2
	;;
esac
for tool in timeout flyctl tar sha256sum; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "human-state-preserve: $tool is required" >&2
		exit 2
	}
done
dir="$(realpath -m "$2")"
if [ "$1" != export ]; then
	if [ ! -s "$dir/manifest.sha256" ] || [ ! -s "$dir/state.tar" ]; then
		refuse "$dir holds no export"
	fi
fi
"do_$1" "$dir"
