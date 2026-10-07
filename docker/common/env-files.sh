#!/bin/sh
set -eu

table=${LAYERX_FILES_TABLE:-/etc/layerx/files.tsv}
role=${LAYERX_ROLE:-}
tab=$(printf '\t')

fail() {
	echo "layerx-env-files: $*" >&2
	exit 1
}

applies() {
	case ",$1," in
	*",*,"* | *",$role,"*) return 0 ;;
	esac
	return 1
}

if [ -f "$table" ]; then
	unset_names=
	while IFS= read -r line || [ -n "$line" ]; do
		name=${line%%"$tab"*}
		rest=${line#*"$tab"}
		guest=${rest%%"$tab"*}
		rest=${rest#*"$tab"}
		mode=${rest%%"$tab"*}
		roles=${rest#*"$tab"}
		case "$line" in
		*"$tab"*"$tab"*"$tab"*) ;;
		'' | '#'*) continue ;;
		*) fail "row '$name' in $table does not have four tab-separated columns" ;;
		esac
		case "$name" in
		'' | '#'*) continue ;;
		*[!A-Za-z0-9_]* | [0-9]*) fail "invalid variable name '$name' in $table" ;;
		esac
		unset_names="$unset_names $name"
		[ -n "$roles" ] || fail "row $name in $table has no roles column"
		applies "$roles" || continue
		case "$guest" in
		/*) ;;
		*) fail "row $name guest path '$guest' is not absolute" ;;
		esac
		mode=${mode:-0600}
		case "$mode" in
		*[!0-7]*) fail "row $name mode '$mode' is not octal" ;;
		esac
		eval "set_=\${$name+x}"
		[ -n "$set_" ] || fail "required variable $name is not set (for $guest)"
		dir=$(dirname "$guest")
		mkdir -p "$dir" || fail "cannot create $dir for $name"
		[ -w "$dir" ] || fail "directory $dir for $name is not writable"
		tmp=$(mktemp "$dir/.layerx-env-files.XXXXXX") || fail "cannot create a temp file in $dir for $name"
		if ! eval "printf '%s' \"\$$name\"" | base64 -d >"$tmp" 2>/dev/null; then
			rm -f "$tmp"
			fail "variable $name is not valid base64 (for $guest)"
		fi
		chmod "$mode" "$tmp" || { rm -f "$tmp"; fail "chmod $mode failed for $name"; }
		if [ -n "${LAYERX_FILES_OWNER:-}" ] && [ "$(id -u)" = 0 ]; then
			chown "$LAYERX_FILES_OWNER" "$tmp" || { rm -f "$tmp"; fail "chown $LAYERX_FILES_OWNER failed for $name"; }
		fi
		mv -f "$tmp" "$guest" || { rm -f "$tmp"; fail "cannot rename into $guest for $name"; }
	done <"$table"
	for name in $unset_names; do
		unset "$name"
	done
fi

[ "$#" -gt 0 ] || fail "no command to exec"
exec "$@"
