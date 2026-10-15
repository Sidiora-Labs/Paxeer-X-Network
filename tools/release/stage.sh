#!/usr/bin/env bash

stage_fail() {
	printf 'stage: %s\n' "$*" >&2
	return 1
}

stage_load_env() {
	local file=$1 line key value number=0
	declare -gA stage_env=()
	[[ -r $file ]] || {
		stage_fail 'env file is not readable'
		return 1
	}
	while IFS= read -r line || [[ -n $line ]]; do
		number=$((number + 1))
		line=${line#"${line%%[![:space:]]*}"}
		[[ -z $line || $line == \#* ]] && continue
		line=${line#export }
		[[ $line =~ ^([A-Z_][A-Z0-9_]*)=(.*)$ ]] || {
			stage_fail "env file line $number is not KEY=VALUE"
			return 1
		}
		key=${BASH_REMATCH[1]}
		value=${BASH_REMATCH[2]}
		[[ " ${stage_keys[*]} " == *" $key "* ]] || continue
		[[ ! -v stage_env[$key] ]] || {
			stage_fail "$key is repeated"
			return 1
		}
		value=${value%"${value##*[![:space:]]}"}
		if [[ $value =~ ^\"(.*)\"$ || $value =~ ^\'(.*)\'$ ]]; then
			value=${BASH_REMATCH[1]}
		fi
		stage_env[$key]=$value
	done <"$file"
	for key in "${stage_keys[@]}"; do
		value=${stage_env[$key]:-}
		[[ -n ${value//[[:space:]]/} ]] || {
			stage_fail "$key is empty"
			return 1
		}
	done
	[[ ${stage_env[RELEASE_ID]} =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]] || {
		stage_fail 'RELEASE_ID is invalid'
		return 1
	}
	[[ ${stage_env[RELEASE_TAG]} == paxeer-network/v6.12.0 ]] || {
		stage_fail 'RELEASE_TAG must be paxeer-network/v6.12.0'
		return 1
	}
	for key in RELEASE_SHA256 SIDECAR_SHA256; do
		[[ ${stage_env[$key]} =~ ^[a-fA-F0-9]{64}$ ]] || {
			stage_fail "$key is invalid"
			return 1
		}
	done
	[[ ${stage_env[HALT_HEIGHT]} =~ ^[1-9][0-9]*$ ]] || {
		stage_fail 'HALT_HEIGHT is invalid'
		return 1
	}
	[[ ${stage_env[BIN_DIR]} == /* ]] || {
		stage_fail 'BIN_DIR must be absolute'
		return 1
	}
}

stage_checksum() {
	local file=$1 expected=$2 name directory
	[[ -f $file && -x $file ]] || {
		stage_fail 'binary is missing or not executable'
		return 1
	}
	name=${file##*/}
	directory=${file%/*}
	(cd -- "$directory" && printf '%s  %s\n' "$expected" "$name" | sha256sum --check --strict --quiet) || {
		stage_fail "checksum mismatch for $name"
		return 1
	}
}

stage_libraries() {
	local output rc=0 library
	output=$(LC_ALL=C ldd -- "$1" 2>&1) || rc=$?
	library=$(awk '/not found/ { print $1; exit }' <<<"$output")
	[[ -z $library ]] || {
		stage_fail "unresolved shared library $library"
		return 1
	}
	[[ $rc == 0 ]] || {
		stage_fail 'ldd failed'
		return 1
	}
}

stage_verify_local() {
	local bin=${stage_env[BIN_DIR]} previous
	previous="$bin/paxd.pre-${stage_env[RELEASE_ID]}"
	stage_checksum "$bin/paxd.v6.12.0" "${stage_env[RELEASE_SHA256]}" || return 1
	stage_libraries "$bin/paxd.v6.12.0" || return 1
	stage_checksum "$bin/price-sidecar" "${stage_env[SIDECAR_SHA256]}" || return 1
	[[ -f $previous && -x $previous && ! -L $previous ]] || {
		stage_fail 'retained binary is missing or invalid'
		return 1
	}
	stage_hosts=1
}

stage_install() {
	local bin=${stage_env[BIN_DIR]} previous destination
	previous="$bin/paxd.pre-${stage_env[RELEASE_ID]}"
	[[ -d $bin && -f $bin/paxd && -x $bin/paxd ]] || {
		stage_fail 'installed paxd is missing or not executable'
		return 1
	}
	exec {stage_lock}>"$bin/.stage.lock" || return 1
	flock -x "$stage_lock" || return 1
	for destination in "$bin/paxd.v6.12.0" "$bin/price-sidecar"; do
		[[ ! -d $destination ]] || {
			stage_fail 'binary destination is a directory'
			return 1
		}
	done
	if [[ -e $previous || -L $previous ]]; then
		[[ -f $previous && -x $previous && ! -L $previous ]] || {
			stage_fail 'retained binary is invalid'
			return 1
		}
	fi
	stage_work=$(mktemp -d "$bin/.stage.XXXXXXXX") || return 1
	install -m 0755 -- ./paxd-v6.12.0-linux-amd64 "$stage_work/paxd.v6.12.0" || return 1
	install -m 0755 -- ./price-sidecar-v6.12.0-linux-amd64 "$stage_work/price-sidecar" || return 1
	stage_checksum "$stage_work/paxd.v6.12.0" "${stage_env[RELEASE_SHA256]}" || return 1
	stage_libraries "$stage_work/paxd.v6.12.0" || return 1
	stage_checksum "$stage_work/price-sidecar" "${stage_env[SIDECAR_SHA256]}" || return 1
	if [[ ! -e $previous ]]; then
		cp -p -- "$bin/paxd" "$stage_work/previous" || return 1
		ln -- "$stage_work/previous" "$previous" || return 1
	fi
	mv -fT -- "$stage_work/paxd.v6.12.0" "$bin/paxd.v6.12.0" || return 1
	mv -fT -- "$stage_work/price-sidecar" "$bin/price-sidecar" || return 1
	stage_hosts=1
}

stage_unit_report() {
	local group=$1 unit=$2 properties=$3 line load= active= enabled=
	while IFS= read -r line; do
		case $line in
		LoadState=*) load=${line#*=} ;;
		ActiveState=*) active=${line#*=} ;;
		UnitFileState=*) enabled=${line#*=} ;;
		esac
	done <<<"$properties"
	[[ ($load == loaded || $load == masked) && -n $active && -n $enabled ]] || {
		stage_fail "cannot inspect $group $unit"
		return 1
	}
	printf 'stage: %s %s %s %s\n' "$group" "$unit" "$active" "$enabled"
	if [[ $active == active ]]; then
		stage_units=$((stage_units + 1))
	fi
	if [[ $group == OLD_FEEDER_UNITS ]]; then
		[[ $active == inactive && ($enabled == disabled || $enabled == masked) ]] || {
			stage_fail "old feeder $unit is not inactive and disabled"
			return 1
		}
	elif [[ $group == AUTOMATION_UNITS ]]; then
		[[ $active == active || $active == inactive ]] || {
			stage_fail "automation unit $unit is not active or inactive"
			return 1
		}
	else
		stage_fail 'unknown unit group'
		return 1
	fi
}

stage_status_local() {
	local group unit properties rc=0
	local -a units=()
	for group in "$@"; do
		read -r -a units <<<"${stage_env[$group]}"
		for unit in "${units[@]}"; do
			if [[ ! $unit =~ ^[a-zA-Z0-9_@.:\\-]+\.(service|timer)$ ]]; then
				stage_fail "invalid unit in $group"
				rc=1
				continue
			fi
			if properties=$(LC_ALL=C systemctl show --no-pager --property=LoadState,ActiveState,UnitFileState -- "$unit"); then
				stage_unit_report "$group" "$unit" "$properties" || rc=1
			else
				stage_fail "cannot inspect $group $unit"
				rc=1
			fi
		done
	done
	return "$rc"
}

stage_remote() {
	local host=$1 operation=$2 file=$3 response rc=0 line counts=0 h u result
	[[ $host =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.@:-]*$ ]] || {
		stage_fail 'invalid inventory host'
		return 1
	}
	response=$({
		printf 'set -euo pipefail\nexport LC_ALL=C\n'
		declare -f stage_fail stage_load_env stage_checksum stage_libraries stage_verify_local stage_unit_report stage_status_local
		declare -p stage_keys
		printf 'stage_hosts=0; stage_units=0; rc=0\n'
		printf 'stage_load_env %q || exit 1\n' "$file"
		printf '[[ ${stage_env[RELEASE_SHA256]} == %q && ${stage_env[SIDECAR_SHA256]} == %q && ${stage_env[RELEASE_ID]} == %q ]] || { stage_fail "release identity differs"; exit 1; }\n' \
			"${stage_env[RELEASE_SHA256]}" "${stage_env[SIDECAR_SHA256]}" "${stage_env[RELEASE_ID]}"
		case $operation in
		verify) printf 'stage_verify_local || rc=1\n' ;;
		automation) printf 'stage_status_local AUTOMATION_UNITS || rc=1\n' ;;
		feeder) printf 'stage_status_local OLD_FEEDER_UNITS || rc=1\n' ;;
		esac
		printf 'printf "stage-counts %%s %%s %%s\\n" "$stage_hosts" "$stage_units" "$rc"\nexit "$rc"\n'
	} | ssh -T -o BatchMode=yes -o ConnectTimeout=10 -- "$host" 'bash -s') || rc=$?
	while IFS= read -r line; do
		if [[ $line =~ ^stage-counts\ ([01])\ ([0-9]+)\ ([01])$ ]]; then
			h=${BASH_REMATCH[1]}
			u=${BASH_REMATCH[2]}
			result=${BASH_REMATCH[3]}
			[[ ${#u} -le 6 ]] || {
				stage_fail 'invalid remote count'
				return 1
			}
			stage_hosts=$((stage_hosts + h))
			stage_units=$((stage_units + 10#$u))
			counts=$((counts + 1))
			[[ $result == 0 ]] || rc=1
		elif [[ -n $line ]]; then
			printf '%s\n' "$line"
		fi
	done <<<"$response"
	[[ $rc == 0 && $counts == 1 ]] || {
		stage_fail 'remote inspection failed'
		return 1
	}
}

stage_fleet() {
	local operation=$1 file=$2 host rc=0
	local -a hosts=()
	local -A seen=()
	read -r -a hosts <<<"${stage_env[VALIDATOR_HOSTS]} ${stage_env[FULLNODE_HOSTS]} ${stage_env[ARCHIVE_HOST]}"
	if [[ $operation == automation-status ]]; then
		stage_remote "${stage_env[OPS_HOST]}" automation "$file" || rc=1
	fi
	for host in "${hosts[@]}"; do
		[[ ! -v seen[$host] ]] || continue
		seen[$host]=1
		if [[ $operation == verify ]]; then
			stage_remote "$host" verify "$file" || rc=1
		else
			stage_remote "$host" feeder "$file" || rc=1
		fi
	done
	return "$rc"
}

stage_finish() {
	local rc=$? result=fail directory temporary
	trap - EXIT
	if [[ -n $stage_work ]]; then
		rm -rf -- "$stage_work" || rc=1
	fi
	[[ $rc == 0 ]] && result=pass
	if [[ -n $stage_receipt ]]; then
		directory=$(dirname -- "$stage_receipt")
		if mkdir -p -- "$directory" && temporary=$(mktemp "$directory/.stage-receipt.XXXXXXXX"); then
			if printf '{"hosts_staged":%d,"units_active":%d,"result":"%s"}\n' \
				"$stage_hosts" "$stage_units" "$result" >"$temporary" && mv -fT -- "$temporary" "$stage_receipt"; then
				:
			else
				rm -f -- "$temporary"
				stage_fail 'cannot write receipt' || true
				rc=1
			fi
		else
			stage_fail 'cannot write receipt' || true
			rc=1
		fi
	fi
	exit "$rc"
}

stage_main() {
	set -euo pipefail
	export LC_ALL=C
	umask 077
	local file=.env.rollout-v6120 mode= local_mode=0 root line tool
	stage_work= stage_receipt= stage_hosts=0 stage_units=0
	while [[ $# -gt 0 ]]; do
		case $1 in
		--env-file | --receipt)
			[[ $# -ge 2 && -n $2 ]] || {
				stage_fail "$1 needs a value"
				return 1
			}
			if [[ $1 == --env-file ]]; then file=$2; else stage_receipt=$2; fi
			shift 2
			;;
		--local)
			local_mode=1
			shift
			;;
		--verify | --check | --automation-status)
			[[ -z $mode ]] || {
				stage_fail 'choose one mode'
				return 1
			}
			mode=${1#--}
			shift
			;;
		*)
			stage_fail "unknown option $1"
			return 1
			;;
		esac
	done
	if [[ -z $mode && $local_mode == 1 ]]; then mode=install; fi
	[[ -n $mode ]] || {
		stage_fail 'choose --local, --verify, --check or --automation-status'
		return 1
	}
	if [[ $mode == check && -n $stage_receipt ]]; then
		stage_fail '--check does not write receipts'
		return 1
	fi
	trap stage_finish EXIT
	root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
	declare -ga stage_keys=()
	while IFS= read -r line || [[ -n $line ]]; do
		[[ $line =~ ^([A-Z_][A-Z0-9_]*)=$ ]] && stage_keys+=("${BASH_REMATCH[1]}")
	done <"$root/env/rollout.env.example"
	[[ ${#stage_keys[@]} -gt 0 ]] || {
		stage_fail 'env template declares no keys'
		return 1
	}
	stage_load_env "$file"
	for tool in sha256sum ldd awk install cp mv ln mktemp flock; do
		command -v "$tool" >/dev/null || {
			stage_fail "$tool is required"
			return 1
		}
	done
	case $mode in
	check) printf 'stage: inputs valid\n' ;;
	install) stage_install ;;
	verify)
		if [[ $local_mode == 1 ]]; then stage_verify_local; else stage_fleet verify "$file"; fi
		;;
	automation-status)
		if [[ $local_mode == 1 ]]; then
			stage_status_local AUTOMATION_UNITS OLD_FEEDER_UNITS
		else
			stage_fleet automation-status "$file"
		fi
		;;
	esac
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
	stage_main "$@"
fi
