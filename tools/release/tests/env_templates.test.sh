#!/usr/bin/env bash
# templates_declare_every_key: every key the release scripts, fleet_check.py and the rollout runbook
# read is declared in tools/release/env/rehearsal.env.example or rollout.env.example (the runbook's in
# rollout.env.example), and every declared key carries an empty value.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

runbook_path=spec/release-v6-12-0/docs/rollout-runbook.md
release_scripts=(rehearse-v6120.sh stage.sh swap.sh snapshot.sh rollback.sh)
shell_builtins=" BASH_REMATCH BASH_SOURCE BASHPID EPOCHSECONDS EUID FUNCNAME HOME HOSTNAME IFS LANG LC_ALL LINENO OLDPWD OPTARG OPTIND PATH PIPESTATUS PWD RANDOM SECONDS SHELL TERM TMPDIR UID USER "

# template_keys <file>: prints the declared keys; refuses a value, a malformed line or a duplicate.
template_keys() {
	local file=$1 line n=0 keys=()
	[ -r "$file" ] || { echo "env_templates: $file is missing" >&2; return 1; }
	while IFS= read -r line || [ -n "$line" ]; do
		n=$((n + 1))
		case $line in '' | '#'*) continue ;; esac
		if [[ $line =~ ^([A-Z_][A-Z0-9_]*)=$ ]]; then
			keys+=("${BASH_REMATCH[1]}")
		elif [[ $line =~ ^([A-Z_][A-Z0-9_]*)= ]]; then
			echo "env_templates: $file:$n: ${BASH_REMATCH[1]} carries a value" >&2
			return 1
		else
			echo "env_templates: $file:$n: not a KEY= line" >&2
			return 1
		fi
	done <"$file"
	[ "${#keys[@]}" -gt 0 ] || { echo "env_templates: $file declares no key" >&2; return 1; }
	local dup
	dup=$(printf '%s\n' "${keys[@]}" | sort | uniq -d)
	[ -z "$dup" ] || { echo "env_templates: $file declares $dup twice" >&2; return 1; }
	printf '%s\n' "${keys[@]}"
}

# shell_keys <file> <sh|md>: names the file reads from its environment: every $NAME or ${NAME it
# references and does not assign itself, every case label of an env-file loader (NAME) NAME=...),
# and for markdown every empty NAME= declaration line.
shell_keys() {
	local file=$1 mode=$2 code refs defs extra
	code=$(grep -v '^[[:space:]]*#' "$file" || true)
	[ "$mode" = md ] && code=$(cat "$file")
	refs=$(grep -oE '\$\{?[A-Z_][A-Z0-9_]*' <<<"$code" | sed 's/^\$[{]\{0,1\}//' | sort -u || true)
	defs=$(awk '
		{
			line = $0
			rest = line
			while (match(rest, /(^|[^A-Za-z0-9_$])[A-Z_][A-Z0-9_]*\+?=/)) {
				m = substr(rest, RSTART, RLENGTH)
				rest = substr(rest, RSTART + RLENGTH)
				sub(/^[^A-Z_]/, "", m)
				sub(/\+?=$/, "", m)
				if (line !~ ("\\$\\{?" m "([^A-Za-z0-9_]|$)")) print m
			}
			rest = line
			while (match(rest, /(read|local|declare|readonly|typeset)( +-[A-Za-z]+)* +[A-Z_][A-Z0-9_]*/)) {
				m = substr(rest, RSTART, RLENGTH)
				rest = substr(rest, RSTART + RLENGTH)
				sub(/.* /, "", m)
				print m
			}
			rest = line
			while (match(rest, /for +[A-Z_][A-Z0-9_]* +in/)) {
				m = substr(rest, RSTART, RLENGTH)
				rest = substr(rest, RSTART + RLENGTH)
				sub(/^for +/, "", m)
				sub(/ +in$/, "", m)
				print m
			}
		}' <<<"$code" | sort -u)
	extra=$(awk -v mode="$mode" '
		{
			rest = $0
			while (match(rest, /(^|[ \t|])[A-Z_][A-Z0-9_]*\)[ \t]*[A-Z_][A-Z0-9_]*=/)) {
				m = substr(rest, RSTART, RLENGTH)
				rest = substr(rest, RSTART + RLENGTH)
				gsub(/[ \t|]/, "", m)
				split(m, p, /\)/)
				sub(/=$/, "", p[2])
				if (p[1] == p[2]) print p[1]
			}
		}
		mode == "md" && /^[A-Z_][A-Z0-9_]*=[ \t]*(#.*)?$/ { sub(/=.*/, ""); print }' <<<"$code")
	{
		comm -23 <(printf '%s\n' "$refs") <(printf '%s\n' "$defs")
		printf '%s\n' "$extra"
	} | sed '/^$/d' | sort -u | while IFS= read -r name; do
		case $shell_builtins in *" $name "*) ;; *) echo "$name" ;; esac
	done
}

# python_keys <file>: env keys a Python script reads by subscript, .get(), getenv() or environ[].
python_keys() {
	grep -oE "(\[|get\(|getenv\()[\"'][A-Z_][A-Z0-9_]*[\"']" "$1" | sed -E "s/^[^\"']*[\"']//; s/[\"']$//" | sort -u || true
}

# require <key> <reader> <declared-keys> <template-name>
require() {
	grep -qxF "$1" <<<"$3" && return 0
	echo "env_templates: $1 is read by $2 and declared in no $4" >&2
	return 1
}

templates_declare_every_key() {
	local base=$1 rollout rehearsal both key script rc=0
	rollout=$(template_keys "$base/tools/release/env/rollout.env.example") || return 1
	rehearsal=$(template_keys "$base/tools/release/env/rehearsal.env.example") || return 1
	both=$(printf '%s\n%s\n' "$rollout" "$rehearsal" | sort -u)
	[ -r "$base/$runbook_path" ] || { echo "env_templates: $runbook_path is missing" >&2; return 1; }
	for key in $(shell_keys "$base/$runbook_path" md); do
		require "$key" "$runbook_path" "$rollout" rollout.env.example || rc=1
	done
	for script in "${release_scripts[@]}"; do
		[ -e "$base/tools/release/$script" ] || continue
		for key in $(shell_keys "$base/tools/release/$script" sh); do
			require "$key" "tools/release/$script" "$both" template || rc=1
		done
	done
	if [ -e "$base/tools/release/fleet_check.py" ]; then
		for key in $(python_keys "$base/tools/release/fleet_check.py"); do
			require "$key" tools/release/fleet_check.py "$both" template || rc=1
		done
	fi
	return "$rc"
}

# The collector itself, on a scratch tree built from the real templates and runbook.
scratch() {
	rm -rf "$work/tree"
	mkdir -p "$work/tree/tools/release/env" "$work/tree/$(dirname "$runbook_path")"
	cp "$root/tools/release/env/rollout.env.example" "$root/tools/release/env/rehearsal.env.example" "$work/tree/tools/release/env/"
	cp "$root/$runbook_path" "$work/tree/$runbook_path"
}
expect_refusal() {
	local what=$1 key=$2
	if templates_declare_every_key "$work/tree" 2>"$work/err"; then fail "$what was accepted"; fi
	grep -q "$key" "$work/err" || fail "$what refusal does not name $key: $(cat "$work/err")"
}

scratch
templates_declare_every_key "$work/tree" || fail "the scratch copy of the tree was refused"

scratch
echo 'Run `"$UNDECLARED_PROBE_KEY"` first.' >>"$work/tree/$runbook_path"
expect_refusal "an undeclared runbook key" UNDECLARED_PROBE_KEY

scratch
grep -v '^SIDECAR_SHA256=$' "$root/tools/release/env/rollout.env.example" >"$work/tree/tools/release/env/rollout.env.example"
expect_refusal "a runbook key missing from rollout.env.example" SIDECAR_SHA256

scratch
sed -i 's/^HALT_HEIGHT=$/HALT_HEIGHT=1/' "$work/tree/tools/release/env/rollout.env.example"
expect_refusal "a template value" HALT_HEIGHT

scratch
echo 'WORK=' >>"$work/tree/tools/release/env/rehearsal.env.example"
expect_refusal "a key declared twice" WORK

scratch
cat >"$work/tree/tools/release/stage.sh" <<'SH'
#!/usr/bin/env bash
NODE_PID=""
BIN_DIR=""
load() { case "$key" in BIN_DIR) BIN_DIR=$value ;; esac; }
RELEASE_ID=${RELEASE_ID:-}
for u in $SIDECAR_UNITS; do echo "$u $NODE_PID $BIN_DIR $HOME"; done
read -ra HOMES <<<"$VALIDATOR_HOMES"
echo "${HOMES[@]} $RELEASE_ID $PORT_BASE"
SH
templates_declare_every_key "$work/tree" || fail "a script reading only declared keys was refused"
echo 'echo "$STAGE_PROBE_KEY"' >>"$work/tree/tools/release/stage.sh"
expect_refusal "an undeclared script key" STAGE_PROBE_KEY
rm "$work/tree/tools/release/stage.sh"
echo 'case "$key" in LOADER_PROBE_KEY) LOADER_PROBE_KEY=$value ;; esac' >"$work/tree/tools/release/rehearse-v6120.sh"
expect_refusal "an undeclared env-file loader key" LOADER_PROBE_KEY

scratch
printf 'import os\nh = env["HALT_HEIGHT"]\nr = os.environ.get("RECEIPT_DIR")\n' >"$work/tree/tools/release/fleet_check.py"
templates_declare_every_key "$work/tree" || fail "a fleet_check.py reading only declared keys was refused"
echo 'p = env.get("FLEET_PROBE_KEY")' >>"$work/tree/tools/release/fleet_check.py"
expect_refusal "an undeclared fleet_check.py key" FLEET_PROBE_KEY

# The real tree.
templates_declare_every_key "$root" || fail "templates_declare_every_key"
for script in "${release_scripts[@]}" fleet_check.py; do
	[ -e "$root/tools/release/$script" ] || echo "env_templates: tools/release/$script not in the tree yet; its keys are checked once it lands"
done
echo "PASS: templates_declare_every_key"
