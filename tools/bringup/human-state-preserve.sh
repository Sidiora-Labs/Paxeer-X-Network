#!/usr/bin/env bash
set -euo pipefail

# The Fly helpers and the app lookup are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/human-state-preserve.sh export <dir> | import <dir> | verify <dir>
       tools/bringup/human-state-preserve.sh restore <dir> <machine>

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
restore <dir> <machine>
              the rollback: puts the export back at the old machine's own
              paths (the map above inverted) on <machine>, a clone of the old
              machine (old image and env) that must be the app's only started
              machine and carries one volume; the phase follows that volume's
              mount path in the machine config. At
              /var/lib/layerx/human-restore: refuses when the volume holds
              anything but lost+found, unpacks human-state/components there
              with the old owners and modes and checks its manifest digests.
              At /var/lib/layerx/human (the mount moved there by a machine
              update, which restarts the old service on it): checks those
              digests again, the old service's /livez on its bind port (8080
              when LAYERX_HUMAN_BIND is unset), refuses when any other carried
              file already exists at its old path, unpacks the rest onto the
              root at /data and /run/human-material and checks every manifest
              digest at its old path. The volume keeps the service's state
              roots across stops; the root copies are reset by a stop as they
              were on the old machine, and a rerun of this phase after a stop
              puts them back.

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

# The old machine's path of a manifest path: the export's map inverted.
# shellcheck disable=SC2016
old_map='old() {
	case "$1" in
	human-state/components/*) echo "/var/lib/layerx/human/${1#human-state/components/}" ;;
	layerx/keys/human-material/*) echo "/run/human-material/${1#layerx/keys/human-material/}" ;;
	*) echo "/data/$1" ;;
	esac
}'

# shellcheck disable=SC2016
stage_absent_cmd='cd /var/lib/layerx/human-restore/ || exit 3
test -z "$(ls -A | grep -vx lost+found)" || exit 4'

# shellcheck disable=SC2016
stage_cmd='cd /var/lib/layerx/ && tar -xpf - --transform "s,^human-state/components\(/\|$\),human-restore\1," human-state/components'

# shellcheck disable=SC2016
stage_verify_cmd='cd /var/lib/layerx/human-restore/ || exit 3
sha256sum --quiet -c - >/dev/null 2>&1 || exit 5'

# shellcheck disable=SC2016
old_verify_cmd="$old_map"'
while read -r h f; do printf "%s  %s\n" "$h" "$(old "$f")"; done | sha256sum --quiet -c - >/dev/null 2>&1 || exit 5'

# shellcheck disable=SC2016
old_absent_cmd="$old_map"'
while read -r h f; do ! test -e "$(old "$f")" || exit 4; done'

# shellcheck disable=SC2016
livez_cmd='p=${LAYERX_HUMAN_BIND##*:}
curl -fsS -o /dev/null --max-time 5 "http://127.0.0.1:${p:-8080}/livez"'

# shellcheck disable=SC2016
root_cmd='t=$(mktemp -d) || exit 1
tar -xpf - -C "$t" --exclude=human-state/components || exit 1
if test -d "$t/layerx/keys/human-material"; then
	mkdir -p /run/human-material && cp -a "$t/layerx/keys/human-material/." /run/human-material/ && rm -rf "$t/layerx/keys/human-material" || exit 1
fi
for m in human-state layerx tls; do ! test -d "$t/$m" || { mkdir -p /data/ && cp -a "$t/$m" /data/; } || exit 1; done
rm -rf "$t"'

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

# restore_phase <app> <machine>: prints stage or old when <machine> is the
# only started machine of the app and its one volume is mounted at the stage
# path or the old path, a refusal reason otherwise.
restore_phase() {
	timeout "$timeout" flyctl machines list --app "$1" --json 2>/dev/null | python3 -c '
import json
import sys

try:
    doc = json.load(sys.stdin)
except ValueError:
    print("machines=unreadable")
    sys.exit(0)
started = [m.get("id", "") for m in doc if m.get("state") == "started"]
if started != [sys.argv[1]]:
    print("started=" + (",".join(started) or "none"))
    sys.exit(0)
machine = next(m for m in doc if m.get("id") == sys.argv[1])
paths = ",".join(x.get("path", "") for x in (machine.get("config") or {}).get("mounts") or [])
print({"/var/lib/layerx/human-restore": "stage", "/var/lib/layerx/human": "old"}.get(paths, "mounts=" + (paths or "none")))
' "$2"
}

do_restore() {
	local dir=$1 machine=$2 app phase rc=0 files parts rest
	check_tar "$dir" || refuse "$dir/state.tar does not match $dir/manifest.sha256"
	app="$(fly_app "$toml")" || refuse "$toml names no app"
	phase="$(restore_phase "$app" "$machine")"
	files="$(grep -c . "$dir/manifest.sha256")"
	parts="$(sed -n 's,^\([0-9a-f]*\)  human-state/components/,\1  ,p' "$dir/manifest.sha256")"
	rest="$(grep -v '^[0-9a-f]*  human-state/components/' "$dir/manifest.sha256" || true)"
	case "$phase" in
	stage)
		fly_ssh "$app" - "$stage_absent_cmd" </dev/null >/dev/null || rc=$?
		case "$rc" in
		0) ;;
		3) refuse "the stage path /var/lib/layerx/human-restore is absent on $machine" ;;
		4) refuse "the volume of $machine already holds state; nothing was overwritten" ;;
		*) refuse "the pre-restore check on $machine failed with status $rc" ;;
		esac
		if [ -n "$parts" ]; then
			fly_ssh "$app" - "$stage_cmd" <"$dir/state.tar" >/dev/null || refuse "staging on $machine failed with status $?"
			fly_ssh "$app" - "$stage_verify_cmd" <<<"$parts" >/dev/null || refuse "a staged file on $machine differs from $dir/manifest.sha256 or is missing"
		fi
		echo "pass restore-stage app=$app machine=$machine files=$(grep -c . <<<"$parts") sha256=match"
		;;
	old)
		if [ -n "$parts" ]; then
			fly_ssh "$app" - "$old_verify_cmd" <<<"$(grep '^[0-9a-f]*  human-state/components/' "$dir/manifest.sha256")" >/dev/null ||
				refuse "a file of the volume of $machine differs from $dir/manifest.sha256 or is missing"
		fi
		echo "pass restore-volume app=$app machine=$machine files=$(grep -c . <<<"$parts") sha256=match"
		fly_ssh "$app" - "$livez_cmd" </dev/null >/dev/null || refuse "the old service on $machine does not answer /livez"
		echo "pass livez app=$app machine=$machine http=200"
		if [ -n "$rest" ]; then
			fly_ssh "$app" - "$old_absent_cmd" <<<"$rest" >/dev/null || rc=$?
			case "$rc" in
			0) ;;
			4) refuse "a carried file already exists at its old path on $machine; nothing was overwritten" ;;
			*) refuse "the pre-restore check on $machine failed with status $rc" ;;
			esac
			fly_ssh "$app" - "$root_cmd" <"$dir/state.tar" >/dev/null || refuse "restoring the root of $machine failed with status $?"
		fi
		fly_ssh "$app" - "$old_verify_cmd" <"$dir/manifest.sha256" >/dev/null ||
			refuse "a file on $machine differs from $dir/manifest.sha256 at its old path or is missing"
		echo "pass restore app=$app machine=$machine files=$files sha256=match"
		;;
	*) refuse "$machine is not a restore target of $app ($phase); it must be the only started machine with one volume at /var/lib/layerx/human-restore or /var/lib/layerx/human" ;;
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

case "$#:${1:-}" in
2:export | 2:import | 2:verify | 3:restore) ;;
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
"do_$1" "$dir" ${3+"$3"}
