#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
box_apply="$root/tools/rollout/box-apply.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
umask 077
mkdir "$tmp/bin" "$tmp/records" "$tmp/no-executables"
export DOCKER_RECORD_DIR="$tmp/records"
export PATH="$tmp/bin:$PATH"
export PYTHONDONTWRITEBYTECODE=1
token_file="$tmp/token file;'quoted'"
actor=registry-user
ref=test-image:0123456789ab
passed=0

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

pass() {
	passed=$((passed + 1))
}

cat >"$tmp/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$1" >>"$DOCKER_RECORD_DIR/calls"
printf '%s\0' "$@" >"$DOCKER_RECORD_DIR/$1.argv"
case "$1" in
login)
	cat >"$DOCKER_RECORD_DIR/login.stdin"
	exit "$DOCKER_LOGIN_EXIT"
	;;
pull) exit 37 ;;
*) exit 97 ;;
esac
DOCKER
chmod 700 "$tmp/bin/docker"
python3 - "$token_file" <<'PY'
import secrets
import sys
from pathlib import Path

Path(sys.argv[1]).write_bytes(secrets.token_bytes(32).hex().encode() + b"\n")
PY

run_box() {
	local file="$1" user="$2" login_exit="$3"
	rm -f "$tmp/records/"*
	status=0
	DOCKER_LOGIN_EXIT="$login_exit" bash "$box_apply" test-role "$ref" test-role.service /healthz \
		"$file" "$user" >"$tmp/output" 2>&1 || status=$?
}

run_box "$token_file" "$actor" 0
[ "$status" -eq 37 ] || fail "successful login did not reach the guarded pull (exit $status)"
printf 'login\npull\n' >"$tmp/want-calls"
cmp -s "$tmp/want-calls" "$tmp/records/calls" || fail "login did not precede exactly one pull"
python3 - "$root/tools/rollout/rollout.py" "$tmp/records" "$token_file" "$tmp/output" "$actor" "$ref" <<'PY' || fail "registry arguments or credential transport differ"
import importlib.util
import sys
from pathlib import Path

source, records, token_file, output, actor, ref = sys.argv[1:]
spec = importlib.util.spec_from_file_location("rollout", source)
rollout = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rollout)
records = Path(records)
registry = rollout.GHCR.split("/", 1)[0]
expected = ["login", registry, "--username", actor, "--password-stdin"]
argv = records.joinpath("login.argv").read_bytes().split(b"\0")
assert argv == [item.encode() for item in expected] + [b""], "login argv mismatch"
argv = records.joinpath("pull.argv").read_bytes().split(b"\0")
assert argv == [b"pull", f"{rollout.GHCR}/{ref}".encode(), b""], "pull argv mismatch"
token = Path(token_file).read_bytes()
assert records.joinpath("login.stdin").read_bytes() == token, "stdin bytes changed"
for path in records.glob("*.argv"):
    assert token.rstrip(b"\n") not in path.read_bytes(), "credential appears in argv"
assert (
    token.rstrip(b"\n") not in Path(output).read_bytes()
), "credential appears in output"
PY
pass

run_box "$token_file" "$actor" 41
[ "$status" -eq 41 ] || fail "login failure exit was $status, want 41"
printf 'login\n' >"$tmp/want-calls"
cmp -s "$tmp/want-calls" "$tmp/records/calls" || fail "failed login performed a pull or container operation"
[ ! -e "$tmp/records/pull.argv" ] || fail "failed login recorded a pull"
grep -Fxq 'registry login refused (exit 41)' "$tmp/output" || fail "failed login refusal was not logged"
cmp -s "$token_file" "$tmp/records/login.stdin" || fail "failed login did not read exact token bytes"
python3 - "$tmp/records/login.argv" "$token_file" "$tmp/output" <<'PY' || fail "failed login leaked credential"
import sys
from pathlib import Path

argv, token_file, output = map(Path, sys.argv[1:])
token = token_file.read_bytes().rstrip(b"\n")
assert token not in argv.read_bytes(), "credential appears in argv"
assert token not in output.read_bytes(), "credential appears in refusal log"
PY
pass

reject_file() {
	local name="$1" file="$2"
	run_box "$file" "$actor" 0
	[ "$status" -eq 2 ] || fail "$name token file exit was $status, want 2"
	[ ! -e "$tmp/records/calls" ] || fail "$name token file invoked Docker"
	grep -q 'registry login refused' "$tmp/output" || fail "$name token file refusal was not logged"
	pass
}

: >"$tmp/empty-token"
mkdir "$tmp/token-directory"
reject_file missing "$tmp/missing-token"
reject_file empty "$tmp/empty-token"
reject_file directory "$tmp/token-directory"
reject_file relative relative-token
reject_file absent ''

run_box "$token_file" 'invalid;user' 0
[ "$status" -eq 2 ] || fail "invalid registry user exit was $status, want 2"
[ ! -e "$tmp/records/calls" ] || fail "invalid registry user invoked Docker"
grep -q 'bad registry user' "$tmp/output" || fail "invalid registry user refusal was not logged"
pass

python3 - "$root/tools/rollout/rollout.py" "$tmp/no-executables" "$tmp" <<'PY' || fail "apply_box configuration refusal differs"
import importlib.util
import io
import os
import sys

source, executable_path, box_reference = sys.argv[1:]
spec = importlib.util.spec_from_file_location("rollout", source)
rollout = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rollout)
os.environ["PATH"] = executable_path
service = {
    "box_role": "test-role",
    "image": "test-image",
    "box_unit": "test-role.service",
    "health": "/healthz",
}
hosts = {rollout.host_key(service["box_role"]): box_reference}
sha = "0123456789abcdef0123456789abcdef01234567"
cases = [
    (
        None,
        "registry-user",
        "REGISTRY_TOKEN_FILE must reference an absolute box-local file",
    ),
    (
        "",
        "registry-user",
        "REGISTRY_TOKEN_FILE must reference an absolute box-local file",
    ),
    (
        "relative-token",
        "registry-user",
        "REGISTRY_TOKEN_FILE must reference an absolute box-local file",
    ),
    (box_reference, None, "GHCR_ACTOR must be a registry username"),
    (box_reference, "", "GHCR_ACTOR must be a registry username"),
    (box_reference, "invalid;user", "GHCR_ACTOR must be a registry username"),
]
for token_file, actor, message in cases:
    for key, value in [("REGISTRY_TOKEN_FILE", token_file), ("GHCR_ACTOR", actor)]:
        if value is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = value
    log = io.StringIO()
    outcome = rollout.apply_box(service, sha, hosts, log)
    assert outcome == "blocked", f"unexpected configuration outcome: {outcome}"
    assert log.getvalue() == message + "\n", "configuration refusal log mismatch"
PY
passed=$((passed + 6))

echo "PASS box registry login: $passed passed, 0 failed"
