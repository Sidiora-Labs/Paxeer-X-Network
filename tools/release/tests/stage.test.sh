#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
stage="$root/tools/release/stage.sh"
source "$stage"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
passed=0
failed=0

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

fixture() {
	case_dir=$(mktemp -d "$work/case.XXXXXXXX")
	mkdir -p "$case_dir/bin" "$case_dir/dist"
	install -m 0755 -- "$(type -P true)" "$case_dir/bin/paxd"
	install -m 0755 -- "$(type -P printf)" "$case_dir/dist/paxd-v6.12.0-linux-amd64"
	install -m 0755 -- "$(type -P sha256sum)" "$case_dir/dist/price-sidecar-v6.12.0-linux-amd64"
	old_sha=$(sha256sum "$case_dir/bin/paxd" | cut -d ' ' -f 1)
	release_sha=$(sha256sum "$case_dir/dist/paxd-v6.12.0-linux-amd64" | cut -d ' ' -f 1)
	sidecar_sha=$(sha256sum "$case_dir/dist/price-sidecar-v6.12.0-linux-amd64" | cut -d ' ' -f 1)
	python3 - "$root/tools/release/env/rollout.env.example" "$case_dir" "$release_sha" "$sidecar_sha" <<'PY'
import pathlib
import sys

template, directory, release, sidecar = sys.argv[1:]
values = {
    "RELEASE_ID": "r1",
    "RELEASE_TAG": "paxeer-network/v6.12.0",
    "RELEASE_SHA256": release,
    "SIDECAR_SHA256": sidecar,
    "HALT_HEIGHT": "20",
    "BIN_DIR": directory + "/bin",
    "AUTOMATION_UNITS": "watchdog.timer resync.timer",
    "OLD_FEEDER_UNITS": "layerx-oracle-feeder.service",
}
lines = []
for line in pathlib.Path(template).read_text().splitlines():
    if line and not line.startswith("#"):
        key = line.split("=", 1)[0]
        lines.append(f"{key}='{values.get(key, 'fixture')}'\n")
pathlib.Path(directory, "rollout.env").write_text("".join(lines))
PY
}

set_input() {
	python3 - "$case_dir/rollout.env" "$1" "$2" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
key, value = sys.argv[2:]
path.write_text("\n".join(
    f"{key}='{value}'" if line.startswith(key + "=") else line
    for line in path.read_text().splitlines()
) + "\n")
PY
}

invoke() {
	(cd -- "$case_dir/dist" && bash "$stage" --env-file "$case_dir/rollout.env" "$@")
}

refusal() {
	local expected=$1
	shift
	local rc=0
	"$@" >"$case_dir/out" 2>"$case_dir/err" || rc=$?
	[[ $rc == 1 ]] || fail "expected exit 1, got $rc"
	grep -Fx -- "$expected" "$case_dir/err" >/dev/null || fail "missing refusal: $expected"
}

assert_sha() {
	[[ $(sha256sum "$1" | cut -d ' ' -f 1) == "$2" ]] || fail 'unexpected binary checksum'
}

assert_receipt() {
	python3 - "$case_dir/receipt.json" "$1" "$2" "$3" <<'PY'
import json
import pathlib
import stat
import sys

path = pathlib.Path(sys.argv[1])
assert json.loads(path.read_text()) == {
    "hosts_staged": int(sys.argv[2]),
    "units_active": int(sys.argv[3]),
    "result": sys.argv[4],
}
assert stat.S_IMODE(path.stat().st_mode) == 0o600
PY
}

stage_keeps_previous_binary() {
	fixture
	invoke --local --receipt "$case_dir/receipt.json"
	assert_sha "$case_dir/bin/paxd" "$old_sha"
	assert_sha "$case_dir/bin/paxd.pre-r1" "$old_sha"
	assert_sha "$case_dir/bin/paxd.v6.12.0" "$release_sha"
	assert_sha "$case_dir/bin/price-sidecar" "$sidecar_sha"
	[[ $(stat -c %a "$case_dir/bin/paxd.v6.12.0") == 755 ]] || fail 'release mode differs'
	assert_receipt 1 0 pass
	retained=$(stat -c '%i:%Y:%a' "$case_dir/bin/paxd.pre-r1")
	cp -- "$case_dir/bin/paxd.v6.12.0" "$case_dir/bin/paxd"
	invoke --local
	assert_sha "$case_dir/bin/paxd.pre-r1" "$old_sha"
	[[ $(stat -c '%i:%Y:%a' "$case_dir/bin/paxd.pre-r1") == "$retained" ]] || fail 'retained binary changed'
	invoke --local --verify --receipt "$case_dir/receipt.json"
	assert_receipt 1 0 pass
}

unresolved_library_refused() {
	fixture
	cat >"$case_dir/tls.c" <<'C'
#include <openssl/ssl.h>

int main(void) {
    SSL_CTX *context = SSL_CTX_new(TLS_method());
    if (context == NULL) {
        return 1;
    }
    SSL_CTX_free(context);
    return 0;
}
C
	cc -Wall -Wextra -Werror -Wl,-z,nodefaultlib "$case_dir/tls.c" -lssl -lcrypto \
		-o "$case_dir/dist/paxd-v6.12.0-linux-amd64"
	set_input RELEASE_SHA256 "$(sha256sum "$case_dir/dist/paxd-v6.12.0-linux-amd64" | cut -d ' ' -f 1)"
	refusal 'stage: unresolved shared library libssl.so.3' invoke --local --receipt "$case_dir/receipt.json"
	[[ ! -e $case_dir/bin/paxd.v6.12.0 && ! -e $case_dir/bin/price-sidecar ]] || fail 'unresolved binary installed'
	[[ ! -e $case_dir/bin/paxd.pre-r1 ]] || fail 'rollback copy created before validation'
	assert_sha "$case_dir/bin/paxd" "$old_sha"
	assert_receipt 0 0 fail
}

old_feeder_reported() {
	fixture
	stage_units=0
	stage_unit_report OLD_FEEDER_UNITS layerx-oracle-feeder.service \
		$'LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled' >"$case_dir/status"
	[[ $(cat "$case_dir/status") == 'stage: OLD_FEEDER_UNITS layerx-oracle-feeder.service inactive disabled' ]] || fail 'old feeder state omitted'
	[[ $stage_units == 0 ]] || fail 'inactive feeder counted active'
	refusal 'stage: old feeder layerx-oracle-feeder.service is not inactive and disabled' \
		stage_unit_report OLD_FEEDER_UNITS layerx-oracle-feeder.service \
		$'LoadState=loaded\nActiveState=active\nUnitFileState=disabled'
	[[ $stage_units == 1 ]] || fail 'active feeder was not counted'
	refusal 'stage: old feeder layerx-oracle-feeder.service is not inactive and disabled' \
		stage_unit_report OLD_FEEDER_UNITS layerx-oracle-feeder.service \
		$'LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled'
	refusal 'stage: cannot inspect OLD_FEEDER_UNITS layerx-oracle-feeder.service' \
		stage_unit_report OLD_FEEDER_UNITS layerx-oracle-feeder.service \
		$'LoadState=not-found\nActiveState=inactive\nUnitFileState='
	stage_units=0
	for unit in watchdog.timer resync.timer; do
		stage_unit_report AUTOMATION_UNITS "$unit" \
			$'LoadState=loaded\nActiveState=active\nUnitFileState=enabled' >>"$case_dir/status"
	done
	[[ $stage_units == 2 ]] || fail 'automation count differs'
	grep -Fx 'stage: AUTOMATION_UNITS watchdog.timer active enabled' "$case_dir/status"
	grep -Fx 'stage: AUTOMATION_UNITS resync.timer active enabled' "$case_dir/status"
	stage_unit_report AUTOMATION_UNITS watchdog.timer \
		$'LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled' >"$case_dir/status"
	[[ $stage_units == 2 ]] || fail 'paused automation counted active'
}

empty_input_refused() {
	local key
	while IFS= read -r key; do
		fixture
		set_input "$key" ''
		refusal "stage: $key is empty" invoke --check
	done < <(sed -n 's/^\([A-Z_][A-Z0-9_]*\)=$/\1/p' "$root/tools/release/env/rollout.env.example")
	fixture
	set_input HALT_HEIGHT '   '
	refusal 'stage: HALT_HEIGHT is empty' invoke --check
	sed -i '/^HALT_HEIGHT=/d' "$case_dir/rollout.env"
	refusal 'stage: HALT_HEIGHT is empty' invoke --check
}

sha_mismatch_refused() {
	local artifact diagnostic
	for artifact in paxd-v6.12.0-linux-amd64 price-sidecar-v6.12.0-linux-amd64; do
		fixture
		printf 'corrupt' >>"$case_dir/dist/$artifact"
		diagnostic=paxd.v6.12.0
		[[ $artifact == price-sidecar-v6.12.0-linux-amd64 ]] && diagnostic=price-sidecar
		refusal "stage: checksum mismatch for $diagnostic" invoke --local --receipt "$case_dir/receipt.json"
		[[ ! -e $case_dir/bin/paxd.v6.12.0 && ! -e $case_dir/bin/price-sidecar ]] || fail 'corrupt release installed'
		[[ ! -e $case_dir/bin/paxd.pre-r1 ]] || fail 'rollback copy created before validation'
		assert_sha "$case_dir/bin/paxd" "$old_sha"
		assert_receipt 0 0 fail
	done
}

check_changes_nothing() {
	fixture
	before=$(find "$case_dir/bin" -printf '%P %s %m\n')
	[[ $(invoke --check) == 'stage: inputs valid' ]] || fail 'check result differs'
	[[ $(find "$case_dir/bin" -printf '%P %s %m\n') == "$before" ]] || fail 'check mutated bin directory'
	refusal 'stage: --check does not write receipts' invoke --check --receipt "$case_dir/receipt.json"
	[[ ! -e $case_dir/receipt.json ]] || fail 'check wrote a receipt'
}

installed_corruption_refused() {
	fixture
	invoke --local
	printf 'corrupt' >>"$case_dir/bin/paxd.v6.12.0"
	refusal 'stage: checksum mismatch for paxd.v6.12.0' invoke --local --verify --receipt "$case_dir/receipt.json"
	assert_receipt 0 0 fail
	fixture
	invoke --local
	printf 'corrupt' >>"$case_dir/bin/price-sidecar"
	refusal 'stage: checksum mismatch for price-sidecar' invoke --local --verify
	fixture
	invoke --local
	rm -- "$case_dir/bin/paxd.pre-r1"
	refusal 'stage: retained binary is missing or invalid' invoke --local --verify
}

retained_symlink_refused() {
	fixture
	ln -s paxd "$case_dir/bin/paxd.pre-r1"
	refusal 'stage: retained binary is invalid' invoke --local
	[[ ! -e $case_dir/bin/paxd.v6.12.0 ]] || fail 'installed despite invalid backup'
	assert_sha "$case_dir/bin/paxd" "$old_sha"
}

env_is_data() {
	fixture
	set_input CHAIN_ID "\$(touch $case_dir/executed)"
	invoke --check
	[[ ! -e $case_dir/executed ]] || fail 'env file executed code'
	set_input RELEASE_ID '../outside'
	refusal 'stage: RELEASE_ID is invalid' invoke --local
	fixture
	printf 'HALT_HEIGHT=21\n' >>"$case_dir/rollout.env"
	refusal 'stage: HALT_HEIGHT is repeated' invoke --check
}

for test in stage_keeps_previous_binary unresolved_library_refused old_feeder_reported \
	empty_input_refused sha_mismatch_refused check_changes_nothing installed_corruption_refused \
	retained_symlink_refused env_is_data; do
	set +e
	(
		set -e
		"$test"
	)
	rc=$?
	set -e
	if [[ $rc == 0 ]]; then
		printf 'PASS: %s\n' "$test"
		passed=$((passed + 1))
	else
		printf 'FAIL: %s (exit %s)\n' "$test" "$rc" >&2
		failed=$((failed + 1))
	fi
done
printf '%s passed, %s failed\n' "$passed" "$failed"
[[ $failed == 0 ]]
