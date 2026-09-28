#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
app_dir=$(dirname -- "$script_dir")
repo_dir=$(CDPATH= cd -- "$app_dir/../../.." && pwd)
names_file="$repo_dir/human/wallet/deploy/env"

cd "$app_dir"

failed=0

step() {
  name=$1
  result=$2
  observed=$3
  printf '%s: %s (%s)\n' "$name" "$result" "$observed"
  if [ "$result" != "pass" ]; then
    failed=1
  fi
}

native_files="capacitor.config.ts
android/build.gradle
android/settings.gradle
android/capacitor.settings.gradle
android/variables.gradle
android/gradle.properties
android/gradlew
android/gradle/wrapper/gradle-wrapper.properties
android/app/build.gradle
android/app/capacitor.build.gradle
android/app/src/main/AndroidManifest.xml
android/app/src/main/java/com/paxeer/wallet/MainActivity.java
ios/App/App.xcodeproj/project.pbxproj
ios/App/Podfile
ios/App/App/AppDelegate.swift
ios/App/App/Info.plist"

present=0
missing=""
for file in $native_files; do
  if [ -s "$file" ]; then
    present=$((present + 1))
  else
    missing="$missing $file"
  fi
done
total=$(printf '%s\n' "$native_files" | wc -l | tr -d ' ')
if [ -z "$missing" ] \
  && grep -q "applicationId \"com.paxeer.wallet\"" android/app/build.gradle \
  && grep -q "PRODUCT_BUNDLE_IDENTIFIER = com.paxeer.wallet;" ios/App/App.xcodeproj/project.pbxproj; then
  step "native projects" pass "$present of $total files, application id com.paxeer.wallet on both platforms"
elif [ -z "$missing" ]; then
  step "native projects" fail "$present of $total files, application id differs from com.paxeer.wallet"
else
  step "native projects" fail "$present of $total files, missing:$missing"
fi

config_module=src/wallet/config.ts
names=$(sed -n '/^export const WALLET_ENV = {/,/^}/p' "$config_module" \
  | grep -o "'[A-Z][A-Z0-9_]*'" | tr -d "'" || true)
if [ -z "$names" ]; then
  step "configuration names" fail "no names read from $config_module"
else
  resolved=0
  unresolved=""
  sources=""
  for name in $names; do
    value=$(printenv "$name" || true)
    if [ -n "$value" ]; then
      source=environment
    elif grep -q "^| \`$name\` |" README.md; then
      source=README.md
    elif [ -f "$names_file" ] && grep -q "^$name - " "$names_file"; then
      source=human/wallet/deploy/env
    else
      source=""
    fi
    if [ -n "$source" ]; then
      resolved=$((resolved + 1))
      sources="$sources $name=$source"
    else
      unresolved="$unresolved $name"
    fi
  done
  count=$(printf '%s\n' "$names" | wc -l | tr -d ' ')
  if [ -z "$unresolved" ]; then
    step "configuration names" pass "$resolved of $count resolved:$sources"
  else
    step "configuration names" fail "$resolved of $count resolved, unresolved:$unresolved"
  fi
fi

if scan_output=$(sh "$script_dir/scan-secrets.sh" "$app_dir" 2>&1); then
  step "secret scan" pass "no findings"
else
  findings=$(printf '%s\n' "$scan_output" | grep -c . || true)
  printf '%s\n' "$scan_output"
  step "secret scan" fail "$findings findings"
fi

playwright="$app_dir/node_modules/.bin/playwright"
if [ ! -x "$playwright" ]; then
  step "playwright check mode" fail "playwright is not installed in node_modules"
elif list_output=$("$playwright" test --config=playwright.wallet-app.config.ts --list 2>&1); then
  listed=$(printf '%s\n' "$list_output" | sed -n 's/^Total: \([0-9][0-9]*\) tests\{0,1\} in \([0-9][0-9]*\) files\{0,1\}$/\1 tests in \2 files/p')
  count=$(printf '%s\n' "$listed" | sed -n 's/^\([0-9][0-9]*\) .*/\1/p')
  if [ -n "$count" ] && [ "$count" -gt 0 ]; then
    step "playwright check mode" pass "$listed"
  else
    printf '%s\n' "$list_output"
    step "playwright check mode" fail "no tests listed"
  fi
else
  printf '%s\n' "$list_output"
  step "playwright check mode" fail "listing exited non-zero"
fi

if [ "${RELEASE_CHECK_BROWSERS:-}" = "1" ]; then
  if "$playwright" test --config=playwright.wallet-app.config.ts; then
    step "playwright suite" pass "every test passed"
  else
    step "playwright suite" fail "the suite exited non-zero"
  fi
fi

if [ "$failed" -ne 0 ]; then
  echo "release-check: fail"
  exit 1
fi
echo "release-check: pass"
