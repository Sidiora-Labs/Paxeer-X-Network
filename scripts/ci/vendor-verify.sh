#!/usr/bin/env bash
#
# vendor-verify.sh
#
# Proves that a vendored directory snapshot equals the upstream commit it is
# pinned to. The pin record <dir>/VENDOR.kvx holds a [vendor] section with
#
#   url = "https://github.com/<owner>/<repo>"
#   ref = "<tag or branch>"
#   sha = "<full 40-hex commit id>"
#
# The script fetches exactly that sha at depth 1 into a fresh temporary
# repository and compares the index entries (git ls-files -s) of both sides:
# every upstream file must be tracked under <dir> with the same blob id, which
# is the same bytes, and the same mode. Under <dir> nothing else may be tracked
# except VENDOR.kvx, no .git may exist, and <dir>/.gitignore must equal the
# upstream .gitignore (or be absent upstream) followed only by negation lines,
# the lines that start with '!'.
#
# Run from anywhere:
#
#   scripts/ci/vendor-verify.sh <dir>
#
# Every difference is printed as the repository-relative path and the reason.
#
# Exit codes: 0 when the snapshot equals upstream, 1 when it differs or the
# fetched commit is not the pinned sha, 2 on a usage error or when the check
# cannot run, 3 when the pin record is missing or malformed, 4 when the pinned
# sha cannot be fetched.

set -euo pipefail

usage() {
    echo "usage: scripts/ci/vendor-verify.sh <dir>" >&2
    exit 2
}

[ "$#" -eq 1 ] || usage

command -v git >/dev/null 2>&1 || {
    echo "vendor-verify: git is required" >&2
    exit 2
}

root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd -P)
cd "$root"

git rev-parse --git-dir >/dev/null 2>&1 || {
    echo "vendor-verify: $root is not a git repository" >&2
    exit 2
}

dir=${1#./}
while [ "${dir%/}" != "$dir" ]; do
    dir=${dir%/}
done
case "$dir" in
    '' | . | /* | .. | ../* | */.. | */../*)
        echo "vendor-verify: $1 is not a root-relative directory" >&2
        exit 2
        ;;
esac
[ -d "$dir" ] || {
    echo "vendor-verify: $dir is not a directory" >&2
    exit 2
}

pin="$dir/VENDOR.kvx"
[ -f "$pin" ] || {
    echo "$pin: pin record missing" >&2
    exit 3
}

pin_value() {
    awk -v want="$1" '
        /^[[:space:]]*\[/ {
            section = $0
            gsub(/^[[:space:]]*\[|\][[:space:]]*(#.*)?$/, "", section)
            next
        }
        section == "vendor" && match($0, "^[[:space:]]*" want "[[:space:]]*=[[:space:]]*\"[^\"]*\"[[:space:]]*(#.*)?$") {
            value = $0
            sub(/^[^"]*"/, "", value)
            sub(/".*$/, "", value)
            print value
            found++
        }
        END { exit found == 1 ? 0 : 1 }
    ' "$pin"
}

for key in url ref sha; do
    value=$(pin_value "$key") || {
        echo "$pin: [vendor] must hold exactly one $key = \"...\" line" >&2
        exit 3
    }
    printf -v "$key" '%s' "$value"
done

case "$url" in
    https://?*) ;;
    *)
        echo "$pin: url must be an https URL, got '$url'" >&2
        exit 3
        ;;
esac
[ -n "$ref" ] || {
    echo "$pin: ref is empty" >&2
    exit 3
}
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || {
    echo "$pin: sha must be a full 40-hex commit id, got '$sha'" >&2
    exit 3
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
upstream="$tmp/upstream"

export GIT_TERMINAL_PROMPT=0
fetch_log="$tmp/fetch.log"
if ! {
    git init -q "$upstream" &&
        git -C "$upstream" fetch -q --depth 1 "$url" "$sha" &&
        git -C "$upstream" -c advice.detachedHead=false checkout -q FETCH_HEAD
} >"$fetch_log" 2>&1; then
    cat "$fetch_log" >&2
    echo "vendor-verify: cannot fetch $sha from $url" >&2
    exit 4
fi

fetched=$(git -C "$upstream" rev-parse HEAD)
if [ "$fetched" != "$sha" ]; then
    echo "$pin: fetched commit $fetched is not the pinned sha $sha"
    exit 1
fi

# Both listings are taken from the root of their own tree, so paths are
# relative to it and quoted the same way on both sides.
git -C "$upstream" -c core.quotePath=false ls-files -s |
    awk -F '\t' '{ split($1, f, " "); print $2 "\t" f[1] " " f[2] }' |
    LC_ALL=C sort >"$tmp/upstream.list"
git -C "$dir" -c core.quotePath=false ls-files -s -- . |
    awk -F '\t' '{ split($1, f, " "); print $2 "\t" f[1] " " f[2] }' |
    LC_ALL=C sort >"$tmp/local.list"

[ -s "$tmp/upstream.list" ] || {
    echo "vendor-verify: $sha of $url lists no file" >&2
    exit 2
}

differs=0

if [ -e "$dir/.git" ]; then
    echo "$dir/.git: must not exist in a snapshot"
    differs=1
fi

if ! grep -q -x -F "VENDOR.kvx" <(cut -f1 "$tmp/local.list"); then
    echo "$pin: not tracked"
    differs=1
fi

LC_ALL=C join -t $'\t' -a 1 -a 2 -e '-' -o '0,1.2,2.2' \
    "$tmp/upstream.list" "$tmp/local.list" >"$tmp/joined"

while IFS=$'\t' read -r path want have; do
    if [ "$path" = "VENDOR.kvx" ] && [ "$want" = "-" ]; then
        continue
    fi
    if [ "$path" = ".gitignore" ]; then
        continue
    fi
    if [ "$have" = "-" ]; then
        echo "$dir/$path: tracked upstream, not tracked here"
    elif [ "$want" = "-" ]; then
        echo "$dir/$path: tracked here, not upstream"
    elif [ "$want" = "$have" ]; then
        continue
    else
        if [ "${want% *}" != "${have% *}" ]; then
            echo "$dir/$path: mode ${have% *}, upstream ${want% *}"
        fi
        if [ "${want#* }" != "${have#* }" ]; then
            echo "$dir/$path: content differs from upstream (blob ${have#* }, upstream ${want#* })"
        fi
    fi
    differs=1
done <"$tmp/joined"

ignore_want=$(awk -F '\t' '$1 == ".gitignore" { print $2 }' "$tmp/joined")
ignore_have=$(awk -F '\t' '$1 == ".gitignore" { print $3 }' "$tmp/joined")

if [ -n "$ignore_have" ] && [ "$ignore_have" != "-" ]; then
    if [ "$ignore_have" = "${ignore_have#100644 }" ]; then
        echo "$dir/.gitignore: mode ${ignore_have% *}, must be 100644"
        differs=1
    fi
    git -C "$dir" cat-file blob "${ignore_have#* }" >"$tmp/ignore.have"
    : >"$tmp/ignore.want"
    if [ -n "$ignore_want" ] && [ "$ignore_want" != "-" ]; then
        git -C "$upstream" cat-file blob "${ignore_want#* }" >"$tmp/ignore.want"
    fi
    want_bytes=$(wc -c <"$tmp/ignore.want")
    if ! cmp -s -n "$want_bytes" "$tmp/ignore.want" "$tmp/ignore.have" ||
        [ "$(wc -c <"$tmp/ignore.have")" -lt "$want_bytes" ]; then
        echo "$dir/.gitignore: does not start with the upstream .gitignore"
        differs=1
    else
        tail -c +"$((want_bytes + 1))" "$tmp/ignore.have" >"$tmp/ignore.tail"
        # An upstream file without a final newline is continued on a new line.
        if [ "$want_bytes" -gt 0 ] && [ -n "$(tail -c 1 "$tmp/ignore.want")" ]; then
            if [ -s "$tmp/ignore.tail" ] && [ "$(head -c 1 "$tmp/ignore.tail" | od -An -tx1 | tr -d ' ')" != "0a" ]; then
                echo "$dir/.gitignore:$(wc -l <"$tmp/ignore.want" | awk '{ print $1 + 1 }'): appended text continues the last upstream line"
                differs=1
                : >"$tmp/ignore.rest"
            else
                tail -c +2 "$tmp/ignore.tail" >"$tmp/ignore.rest"
            fi
            offset=$(($(wc -l <"$tmp/ignore.want") + 1))
        else
            cp "$tmp/ignore.tail" "$tmp/ignore.rest"
            offset=$(wc -l <"$tmp/ignore.want")
        fi
        bad=$(awk -v base="$offset" -v file="$dir/.gitignore" '
            substr($0, 1, 1) != "!" { printf "%s:%d: appended line is not a negation\n", file, base + NR }
        ' "$tmp/ignore.rest")
        if [ -n "$bad" ]; then
            printf '%s\n' "$bad"
            differs=1
        fi
    fi
elif [ -n "$ignore_want" ] && [ "$ignore_want" != "-" ]; then
    echo "$dir/.gitignore: tracked upstream, not tracked here"
    differs=1
fi

if [ "$differs" -ne 0 ]; then
    echo "vendor-verify: $dir differs from $url at $sha ($ref)" >&2
    exit 1
fi
echo "vendor-verify: $dir equals $url at $sha ($ref)"
