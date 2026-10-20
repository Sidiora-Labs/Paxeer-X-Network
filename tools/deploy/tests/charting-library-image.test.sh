#!/usr/bin/env bash
set -euo pipefail

script="$(cd "$(dirname "$0")/.." && pwd)/charting-library-image.sh"
image=local/charting-library-image-test
work=$(mktemp -d)
trap 'rm -rf "$work"; docker image rm -f $(docker images -q "$image" | sort -u) >/dev/null 2>&1 || true' EXIT

mkdir -p "$work/src/charting_library" "$work/src/datafeeds/udf"
echo 'fake entry' > "$work/src/charting_library/charting_library.js"
echo 'fake datafeed' > "$work/src/datafeeds/udf/index.js"

fail() { echo "FAIL: $*" >&2; exit 1; }

set +e
bash "$script" --source "$work/missing" --image "$image" >/dev/null 2>&1; [ $? -eq 2 ] || fail "missing source must exit 2"
set -e

tag1=$(bash "$script" --source "$work/src" --image "$image")
tag2=$(bash "$script" --source "$work/src" --image "$image")
[[ "$tag1" =~ ^[0-9a-f]{64}$ ]] || fail "tag not a sha256: $tag1"
[ "$tag1" = "$tag2" ] || fail "tag not deterministic: $tag1 vs $tag2"

echo changed > "$work/src/datafeeds/udf/index.js"
tag3=$(bash "$script" --source "$work/src" --image "$image")
[ "$tag3" != "$tag1" ] || fail "tag did not change with content"

docker image inspect "$image:latest" >/dev/null || fail "latest tag missing"

mkdir "$work/save"
docker save "$image:$tag1" | tar -x -C "$work/save"
listing=$(find "$work/save" -type f -exec sh -c 'tar -tf "$1" 2>/dev/null' _ {} \;)
grep -qx 'charting_library/charting_library.js' <<<"$listing" || fail "image lacks /charting_library/charting_library.js"
grep -qx 'datafeeds/udf/index.js' <<<"$listing" || fail "image lacks /datafeeds/udf/index.js"

echo "PASS charting-library-image $tag1"
