#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(dirname -- "$script_dir")

if [ "$#" -ne 1 ]; then
  echo "usage: release-build.sh android|ios" >&2
  exit 2
fi

platform=$1
case "$platform" in
  android | ios) ;;
  *)
    echo "release-build: unsupported platform: $platform (expected android or ios)" >&2
    exit 2
    ;;
esac

cd "$app_dir"

pnpm run build

web_dir=out
rm -rf "$web_dir"
mkdir -p "$web_dir"
cp -R public/. "$web_dir/"

npx cap sync "$platform"

echo "release-build: $platform synced from $web_dir, unsigned"
