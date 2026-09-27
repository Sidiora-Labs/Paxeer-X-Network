#!/bin/sh
#
# path-length-check.sh
#
# Keeps every tracked path short enough to be checked out on every platform the
# workflows build on. Windows resolves a path of at most 260 characters unless
# long paths are enabled for the whole machine, and a runner checks the
# repository out into a workspace directory of its own, so what a
# repository-relative path may spend is what is left of those 260 characters
# after that workspace prefix. Two bounds keep it inside the budget: a
# repository-relative path of at most 200 characters, which leaves room for the
# workspace prefix and the separator, and a basename of at most 120 characters,
# so a file can still be moved into a deeper directory without breaking the
# first bound.
#
# Run from anywhere:
#
#   tools/ci/path-length-check.sh
#
# Exit codes: 0 when every tracked path is inside both bounds, 1 when one is
# not, 2 when the check cannot run.

set -eu

max_basename=120
max_path=200

root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd -P)
cd "$root"

command -v git >/dev/null 2>&1 || {
    echo "path-length-check: git is required" >&2
    exit 2
}

git rev-parse --git-dir >/dev/null 2>&1 || {
    echo "path-length-check: $root is not a git repository" >&2
    exit 2
}

tracked=$(mktemp)
measured=$(mktemp)
trap 'rm -f "$tracked" "$measured"' EXIT HUP INT TERM

git -c core.quotePath=false ls-files --cached -z | tr '\0' '\n' > "$tracked"

if [ ! -s "$tracked" ]; then
    echo "path-length-check: the index lists no file" >&2
    exit 2
fi

awk -v max_basename="$max_basename" -v max_path="$max_path" '
{
    path_length = length($0)
    components = split($0, component, "/")
    basename_length = length(component[components])

    if (basename_length > longest_basename_length) {
        longest_basename_length = basename_length
        longest_basename = $0
    }
    if (path_length > longest_path_length) {
        longest_path_length = path_length
        longest_path = $0
    }

    if (basename_length > max_basename) {
        printf "offender\tbasename %d characters, at most %d allowed: %s\n", basename_length, max_basename, $0
    }
    if (path_length > max_path) {
        printf "offender\tpath %d characters, at most %d allowed: %s\n", path_length, max_path, $0
    }
}
END {
    printf "longest-basename\t%d characters: %s\n", longest_basename_length, longest_basename
    printf "longest-path\t%d characters: %s\n", longest_path_length, longest_path
}
' "$tracked" > "$measured"

offenders=$(grep -c '^offender	' "$measured" || true)
checked=$(wc -l < "$tracked" | tr -d ' ')

if [ "$offenders" -gt 0 ]; then
    printf 'path-length-check: %s tracked path(s) outside the checkout bounds\n' "$offenders" >&2
    sed -n 's/^offender	/  /p' "$measured" >&2
    printf 'path-length-check: a basename may be at most %s characters and a repository-relative path at most %s\n' \
        "$max_basename" "$max_path" >&2
    exit 1
fi

printf 'path-length-check: %s tracked paths inside the bounds (basename <= %s, path <= %s)\n' \
    "$checked" "$max_basename" "$max_path"
sed -n 's/^longest-basename	/path-length-check: longest basename /p' "$measured"
sed -n 's/^longest-path	/path-length-check: longest path /p' "$measured"
