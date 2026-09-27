#!/usr/bin/env bash
# Replays an upgrade plan over a copy of a chain's state and prints every
# assertion the fork owes that chain.
#
#   run.sh                     generate a pre-fork fixture and replay the latest plan over it
#   run.sh <data-dir>          replay the latest plan over a copy of <data-dir>
#   run.sh <data-dir> <plan>   replay <plan> over a copy of <data-dir>
#
# The replay writes to the state it runs over, so it copies the data directory
# first; UPGRADE_REPLAY_IN_PLACE=1 replays over the directory itself, which only
# suits a copy that is already disposable. UPGRADE_REPLAY_KEEP_STORES=1 makes the
# generated fixture keep the fork modules' stores, the shape of a chain that
# synced its pre-fork history with a binary already mounting them.
# UPGRADE_REPLAY_WORKDIR chooses where the copy, the fixture and the binary live.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
data=${1:-}
plan=${2:-}
workdir=${UPGRADE_REPLAY_WORKDIR:-${TMPDIR:-/tmp}/paxeer-upgrade-replay}
binary="$workdir/upgrade-replay"
home="$workdir/home"
logdir="$root/build/upgrade-replay"
label=${plan:-activation}

mkdir -p "$workdir" "$logdir"
go build -C "$root" -o "$binary" ./tools/chain/upgrade-replay/

if [ -z "$data" ]; then
	keep=${UPGRADE_REPLAY_KEEP_STORES:-0}
	if [ "$keep" = "1" ]; then
		fixture="$workdir/fixture-kept"
		label="kept-fixture-$label"
	else
		fixture="$workdir/fixture"
		label="fixture-$label"
	fi
	if [ ! -d "$fixture/data" ]; then
		echo "no data directory given; generating a pre-fork fixture under $fixture"
		args=(-mode fixture -out "$fixture")
		if [ "$keep" = "1" ]; then
			args+=(-keep-stores)
		fi
		"$binary" "${args[@]}" 2>&1 | tee "$logdir/$label-genesis.log"
	else
		echo "reusing the pre-fork fixture under $fixture"
	fi
	data="$fixture/data"
else
	label="state-$label"
fi
log="$logdir/$label.log"

if [ "${UPGRADE_REPLAY_IN_PLACE:-0}" = "1" ]; then
	state=$data
	echo "replaying over $state itself"
else
	state="$workdir/state"
	rm -rf "$state"
	echo "copying $data to $state, because a replay writes to the state it runs over"
	cp -a "$data" "$state"
fi

rm -rf "$home"
args=(-mode replay -data "$state" -home "$home")
if [ -n "$plan" ]; then
	args+=(-plan "$plan")
fi

"$binary" "${args[@]}" 2>&1 | tee "$log"
