#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The probe lends its host map loader, its ssh and RPC helpers and the fleet
# constants rpc_name_count, rpc_max_lag and rpc_domain.
# shellcheck disable=SC1091
. "$here/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/sync-fleet.sh [--dry-run] config|restart|resync <paxd>

Brings the public RPC fleet back onto the head of the chain. Every
subcommand reads the operator's private host map from the file named by
BRINGUP_HOSTS_FILE, as tools/bringup/check-live.sh does, and never prints a
value from it: a host is named by its role label (RPC_HOSTS[k], ARCHIVE_HOST)
and by the public name its nginx serves. Only the full-node unit paxd.service
is ever touched; a validator unit never is. With --dry-run the read-only
questions (which name a host serves, its head, its unit state) are still
asked and every mutating remote command is printed instead of run.

config    on every RPC_HOSTS destination and the archive host, backs the node
          config up beside itself as config.toml.bak-<utc stamp> and sets the
          three lag keys of its [self-remediation] section to
          blocks-behind-threshold 200, blocks-behind-check-interval 30 and
          restart-cooldown-seconds 300. Prints one line per host:
  "pass config <label> <apiN> blocks-behind-threshold=200
   blocks-behind-check-interval=30 restart-cooldown-seconds=300
   backup=config.toml.bak-<stamp> unit=<state>" when the file now carries
  the three values, "fail config <label> ..." otherwise.

restart   asks the sixteen public names for eth_blockNumber at once and, for
          every RPC_HOSTS destination whose head trails the highest answer by
          more than 200 blocks but by no more than the 100000 blocks the
          pruned peers retain, restarts paxd.service so block sync refills
          the gap. Prints one line per host:
  "restart <label> <apiN> lag=<blocks> unit=<state>" for a restarted node,
  "keep <label> <apiN> lag=<blocks>" for a node within 200 blocks,
  "skip <label> <apiN> lag=<blocks> beyond the retain window" for a node
  that needs resync, "fail <label> ..." when the host could not be asked.

resync <paxd>
          checks that <paxd> is the release the fleet runs (its sha256 must
          equal the one written into this script), then for every RPC_HOSTS
          destination whose gap exceeds the retain window: picks the synced
          full node with the highest head that is neither the archive host
          nor a validator host as the source, checks the target has room for
          the source's data directory plus a tenth, stops the source, copies
          its data directory (without priv_validator_state.json and
          snapshots/) into data.incoming-<stamp> on the target, with rsync
          straight from source to target when the source can ssh to it and
          as a tar stream through this box otherwise, starts the source and
          waits for it to return within ten blocks of the head, then on the
          target stops paxd.service, keeps its own priv_validator_state.json,
          moves data aside as data.stale-<stamp>, moves the copy into place,
          installs <paxd> as /usr/local/bin/paxd (the old one kept as
          paxd.pre-<stamp>) when the host's binary differs, starts the unit
          and waits for it to come within ten blocks. Prints one line per
          target:
  "resync <label> <apiN> source=<apiM> mode=rsync|stream binary=installed|kept
   stale=data.stale-<stamp> unit=<state> lag=<blocks>" on success,
  "fail resync <label> <apiN> <reason>" otherwise. The archive host and a
  validator host are never resync targets.

Environment:
  BRINGUP_HOSTS_FILE  the private host map, required
  CHECK_LIVE_TIMEOUT  seconds per request or short remote command, default 30
  SYNC_FLEET_SETTLE   seconds to wait for a restarted node to come within ten
                      blocks of the head, default 1800
  HPX_HOME            the node home on every host, default /root/.paxeer
  PAXD                the node binary on every host, default /usr/local/bin/paxd

Exits 0 when every line passed, 1 when any failed, 2 on a usage error or a
binary that is not the release.
EOF
}

release_sha256=f80feec8fa66f7b0eabfe6bd5bf664ffc55f120edc2b84864d3c5bde78f1b6cb
lag_threshold=200
check_interval=30
restart_cooldown=300
retain_window=100000
headroom_pct=10
settle="${SYNC_FLEET_SETTLE:-1800}"
home="${HPX_HOME:-/root/.paxeer}"
paxd="${PAXD:-/usr/local/bin/paxd}"
unit=paxd.service
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
dry_run=0
failures=0

# say <line>: one result line on stdout.
say() {
	echo "$*"
}

fail() {
	echo "fail $*"
	failures=$((failures + 1))
}

# remote <label> <destination> <command> [<embedded destination> <its label>]:
# runs a mutating command on the destination without prompting; under
# --dry-run prints "ssh -- <label> <command>" with any embedded destination
# replaced by its label instead. Returns the ssh exit code.
remote() {
	local label="$1" dest="$2" cmd="$3" shown="$3"
	if [ "$dry_run" -eq 1 ]; then
		[ "$#" -lt 5 ] || shown="${shown//"$4"/$5}"
		echo "ssh -- $label $shown"
		return 0
	fi
	ssh -n -o BatchMode=yes -- "$dest" "$cmd"
}

# poll_heads: asks the sixteen public names for eth_blockNumber at once and
# fills heads[<apiN>] plus top, the highest answer.
poll_heads() {
	local n polls
	polls="$(mktemp -d)"
	# shellcheck disable=SC2154
	for n in $(seq 1 "$rpc_name_count"); do
		rpc_head "api$n" >"$polls/$n" 2>/dev/null &
	done
	wait
	top=0
	for n in $(seq 1 "$rpc_name_count"); do
		heads[api$n]="$(cat "$polls/$n")"
		if [ -n "${heads[api$n]}" ] && [ "${heads[api$n]}" -gt "$top" ]; then
			top="${heads[api$n]}"
		fi
	done
	rm -rf "$polls"
}

# lag_of <apiN>: the name's distance behind the highest answer, or none.
lag_of() {
	if [ -n "${heads[$1]:-}" ]; then
		echo $((top - heads[$1]))
	else
		echo none
	fi
}

# survey: fills dests[] from RPC_HOSTS, names[k] with the public name each
# destination serves (none when it could not be asked or serves no site) and
# ssh_status[k] with the exit code of asking, then polls the heads.
survey() {
	local k reply status name
	read -r -a dests <<<"$RPC_HOSTS"
	for k in "${!dests[@]}"; do
		status=0
		reply="$(rpc_unit "${dests[$k]}")" || status=$?
		name=none
		if [ "$status" -eq 0 ]; then
			read -r name _ <<<"$reply"
			case "$name" in
			api[1-9] | api1[0-6]) ;;
			*) name=none ;;
			esac
		fi
		names[k]="$name"
		ssh_status[k]="$status"
	done
	poll_heads
}

# wait_within <apiN>: polls until the name is within ten blocks of the
# highest answer or SYNC_FLEET_SETTLE seconds have passed; prints the final
# lag and returns 1 on the timeout.
wait_within() {
	local start=$SECONDS lag
	while :; do
		poll_heads
		lag="$(lag_of "$1")"
		# shellcheck disable=SC2154
		if [ "$lag" != none ] && [ "$lag" -le "$rpc_max_lag" ]; then
			echo "$lag"
			return 0
		fi
		if [ $((SECONDS - start)) -ge "$settle" ]; then
			echo "$lag"
			return 1
		fi
		sleep 10
	done
}

is_member() {
	local h
	for h in $2; do
		[ "$h" = "$1" ] && return 0
	done
	return 1
}

config_cmd() {
	local cfg="$home/config/config.toml" edit="" show="" key
	for key in "blocks-behind-threshold $lag_threshold" "blocks-behind-check-interval $check_interval" "restart-cooldown-seconds $restart_cooldown"; do
		edit="${edit}s/^${key% *} = .*/${key% *} = ${key#* }/;"
		show="${show}${show:+\\|}${key% *}"
	done
	printf 'set -e; cp -p %s %s.bak-%s; sed -i "/^\\[self-remediation\\]/,/^\\[/{%s}" %s; sed -n "/^\\[self-remediation\\]/,/^\\[/{s/^\\(%s\\) = \\(.*\\)/\\1=\\2/p}" %s | tr "\\n" " "; systemctl show %s -p ActiveState --value' \
		"$cfg" "$cfg" "$stamp" "$edit" "$cfg" "$show" "$cfg" "$unit"
}

config_host() {
	local label="$1" dest="$2" reply status=0 want name wrote
	want="blocks-behind-threshold=$lag_threshold blocks-behind-check-interval=$check_interval restart-cooldown-seconds=$restart_cooldown"
	name="$(rpc_unit "$dest" | cut -d' ' -f1)" || true
	reply="$(remote "$label" "$dest" "$(config_cmd)")" || status=$?
	if [ "$dry_run" -eq 1 ]; then
		say "$reply"
		return 0
	fi
	if [ "$status" -ne 0 ]; then
		fail "config $label ${name:-none} ssh=$status"
		return 0
	fi
	wrote="${reply% *}"
	[ "$wrote" != "$reply" ] || wrote=""
	if [ "$wrote" = "$want" ]; then
		say "pass config $label ${name:-none} $want backup=config.toml.bak-$stamp unit=${reply##* }"
	else
		fail "config $label ${name:-none} wrote=$wrote want=$want unit=${reply##* }"
	fi
}

cmd_config() {
	local -a dests
	local k
	read -r -a dests <<<"$RPC_HOSTS"
	for k in "${!dests[@]}"; do
		config_host "RPC_HOSTS[$k]" "${dests[$k]}"
	done
	is_member "$ARCHIVE_HOST" "$RPC_HOSTS" || config_host ARCHIVE_HOST "$ARCHIVE_HOST"
}

cmd_restart() {
	local -a dests names ssh_status
	local -A heads
	local top k label lag state status=0
	survey
	for k in "${!dests[@]}"; do
		label="RPC_HOSTS[$k]"
		if [ "${ssh_status[$k]}" -ne 0 ]; then
			fail "$label ssh=${ssh_status[$k]}"
			continue
		fi
		if [ "${names[$k]}" = none ]; then
			fail "$label site=none"
			continue
		fi
		lag="$(lag_of "${names[$k]}")"
		if [ "$lag" = none ]; then
			fail "$label ${names[$k]} head=none"
		elif [ "$lag" -gt "$retain_window" ]; then
			say "skip $label ${names[$k]} lag=$lag beyond the retain window"
		elif [ "$lag" -gt "$lag_threshold" ]; then
			status=0
			state="$(remote "$label" "${dests[$k]}" "systemctl restart $unit && systemctl show $unit -p ActiveState --value")" || status=$?
			if [ "$dry_run" -eq 1 ]; then
				say "$state"
			elif [ "$status" -ne 0 ]; then
				fail "$label ${names[$k]} lag=$lag ssh=$status"
			else
				say "restart $label ${names[$k]} lag=$lag unit=$state"
			fi
		else
			say "keep $label ${names[$k]} lag=$lag"
		fi
	done
}

# resync_target <k> <source k>: the whole copy for one target; prints its
# line. The source is started again whatever the copy did, and the target is
# started again whatever the swap did.
resync_target() {
	local k="$1" label="RPC_HOSTS[$1]" dest="${dests[$1]}" src="${dests[$2]}" slabel="RPC_HOSTS[$2]"
	local name="${names[$1]}" sname="${names[$2]}" inc="data.incoming-$stamp" stale="data.stale-$stamp"
	local size free need mode status=0 lag sha binary=kept state
	local excludes="--exclude=priv_validator_state.json --exclude=snapshots"
	if [ "$dest" = "$ARCHIVE_HOST" ]; then
		fail "resync $label $name the archive host keeps its history"
		return 0
	fi
	if is_member "$dest" "$VALIDATOR_HOSTS"; then
		fail "resync $label $name a validator host is never resynced"
		return 0
	fi
	size="$(ssh_read "$src" "du -sb --exclude=snapshots $home/data | cut -f1")" || status=$?
	free="$(ssh_read "$dest" "df -B1 --output=avail $home | tail -n 1 | tr -d ' '")" || status=$?
	if [ "$status" -ne 0 ] || ! [[ "$size" =~ ^[0-9]+$ ]] || ! [[ "$free" =~ ^[0-9]+$ ]]; then
		fail "resync $label $name source=$sname sizing failed ssh=$status"
		return 0
	fi
	need=$((size + size * headroom_pct / 100))
	if [ "$free" -lt "$need" ]; then
		fail "resync $label $name source=$sname free=$free need=$need"
		return 0
	fi
	mode=stream
	if ssh_read "$src" "ssh -n -o BatchMode=yes -o ConnectTimeout=8 -- $dest true" >/dev/null; then
		mode=rsync
	fi
	if ! remote "$slabel" "$src" "systemctl stop $unit"; then
		fail "resync $label $name source=$sname the source unit did not stop"
		return 0
	fi
	if [ "$mode" = rsync ]; then
		remote "$slabel" "$src" "rsync -a --partial $excludes $home/data/ $dest:$home/$inc/" "$dest" "$label" || status=$?
	elif [ "$dry_run" -eq 1 ]; then
		say "ssh -n -- $slabel tar -C $home $excludes -cf - data | ssh -- $label mkdir -p $home/$inc && tar -C $home/$inc --strip-components=1 -xf -"
	else
		ssh -n -o BatchMode=yes -- "$src" "tar -C $home $excludes -cf - data" |
			ssh -o BatchMode=yes -- "$dest" "mkdir -p $home/$inc && tar -C $home/$inc --strip-components=1 -xf -" || status=$?
	fi
	remote "$slabel" "$src" "systemctl start $unit" || status=$?
	if [ "$status" -ne 0 ]; then
		fail "resync $label $name source=$sname mode=$mode copy failed exit=$status"
		return 0
	fi
	if [ "$dry_run" -eq 1 ]; then
		say "wait until $sname is within $rpc_max_lag blocks of the head"
	elif ! lag="$(wait_within "$sname")"; then
		fail "resync $label $name source=$sname lag=$lag the source did not return to the head"
		return 0
	fi
	sha="$(ssh_read "$dest" "sha256sum $paxd | cut -d' ' -f1")" || true
	remote "$label" "$dest" "set -e; test -d $home/$inc/tendermint/blockstore.db; test -d $home/$inc/tendermint/state.db; test -d $home/$inc/state_store; systemctl stop $unit; [ ! -f $home/data/priv_validator_state.json ] || cp -a $home/data/priv_validator_state.json $home/$inc/; mv $home/data $home/$stale; mv $home/$inc $home/data" || status=$?
	if [ "$status" -eq 0 ] && [ "$sha" != "$release_sha256" ]; then
		binary=installed
		if [ "$dry_run" -eq 1 ]; then
			say "scp -q -- $bin $label:$paxd.new-$stamp"
		else
			scp -q -o BatchMode=yes -- "$bin" "$dest:$paxd.new-$stamp" || status=$?
		fi
		[ "$status" -ne 0 ] || remote "$label" "$dest" "set -e; [ \"\$(sha256sum $paxd.new-$stamp | cut -d' ' -f1)\" = $release_sha256 ]; cp -p $paxd $paxd.pre-$stamp; chmod 0755 $paxd.new-$stamp; mv -f $paxd.new-$stamp $paxd" || status=$?
	fi
	state="$(remote "$label" "$dest" "systemctl start $unit && systemctl show $unit -p ActiveState --value")" || status=$?
	if [ "$status" -ne 0 ]; then
		fail "resync $label $name source=$sname mode=$mode binary=$binary stale=$stale unit=$state swap failed exit=$status"
		return 0
	fi
	if [ "$dry_run" -eq 1 ]; then
		say "$state"
		say "wait until $name is within $rpc_max_lag blocks of the head"
		say "resync $label $name source=$sname mode=$mode binary=$binary stale=$stale"
	elif lag="$(wait_within "$name")"; then
		say "resync $label $name source=$sname mode=$mode binary=$binary stale=$stale unit=$state lag=$lag"
	else
		fail "resync $label $name source=$sname mode=$mode binary=$binary stale=$stale unit=$state lag=$lag"
	fi
}

cmd_resync() {
	local -a dests names ssh_status targets=()
	local -A heads
	local top k lag src="" best=-1
	survey
	for k in "${!dests[@]}"; do
		[ "${names[$k]}" != none ] || continue
		lag="$(lag_of "${names[$k]}")"
		[ "$lag" != none ] || continue
		if [ "$lag" -gt "$retain_window" ]; then
			targets+=("$k")
		elif [ "$lag" -le "$rpc_max_lag" ] && [ "${dests[$k]}" != "$ARCHIVE_HOST" ] && ! is_member "${dests[$k]}" "$VALIDATOR_HOSTS" &&
			[ "${heads[${names[$k]}]}" -gt "$best" ]; then
			best="${heads[${names[$k]}]}"
			src="$k"
		fi
	done
	if [ "${#targets[@]}" -eq 0 ]; then
		say "resync none beyond the retain window"
		return 0
	fi
	if [ -z "$src" ]; then
		fail "resync no synced full node to copy from"
		return 0
	fi
	for k in "${targets[@]}"; do
		resync_target "$k" "$src"
	done
}

# The dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

if [ "${1:-}" = --dry-run ]; then
	dry_run=1
	shift
fi
mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
config | restart)
	[ "$#" -eq 1 ] || {
		usage >&2
		exit 2
	}
	;;
resync)
	[ "$#" -eq 2 ] || {
		usage >&2
		exit 2
	}
	bin="$2"
	if [ ! -f "$bin" ]; then
		echo "sync-fleet: $bin is not a file" >&2
		exit 2
	fi
	sha="$(sha256sum "$bin" | cut -d' ' -f1)"
	if [ "$sha" != "$release_sha256" ]; then
		echo "sync-fleet: $bin has sha256 $sha, not the release $release_sha256" >&2
		exit 2
	fi
	;;
*)
	usage >&2
	exit 2
	;;
esac

for tool in ssh scp timeout curl sha256sum; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "sync-fleet: $tool is required" >&2
		exit 2
	fi
done

load_hosts
"cmd_$mode"
if [ "$failures" -ne 0 ]; then
	echo "sync-fleet: $failures host(s) failed"
	exit 1
fi
echo "sync-fleet: all hosts passed"
