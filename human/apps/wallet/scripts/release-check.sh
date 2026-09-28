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

native=""
for path in capacitor.config.ts ios android; do
  if [ -e "$path" ]; then
    native="$native $path"
  fi
done
if grep -q '"@capacitor/' package.json; then
  native="$native package.json:@capacitor"
fi
if [ -z "$native" ]; then
  step "native scaffolding" pass "no native project, runtime configuration or dependency"
else
  step "native scaffolding" fail "present:$native"
fi

if build_output=$(node scripts/build-pwa.mjs 2>&1); then
  version=$(printf '%s\n' "$build_output" | sed -n 's/.* version \([0-9a-f]\{16\}\),.*/\1/p')
  if [ -n "$version" ] && [ -s public/sw.js ] && grep -q "$version" public/sw.js \
    && grep -q 'SKIP_WAITING' public/sw.js; then
    step "service worker built" pass "public/sw.js version $version"
  else
    printf '%s\n' "$build_output"
    step "service worker built" fail "public/sw.js missing, empty or without version $version"
  fi
else
  printf '%s\n' "$build_output"
  step "service worker built" fail "build-pwa exited non-zero"
fi

if manifest_output=$(node --input-type=module -e '
import { readFileSync, existsSync } from "node:fs";
const manifest = JSON.parse(readFileSync("public/manifest.json", "utf8"));
const problems = [];
for (const key of ["id", "name", "short_name", "start_url", "scope", "background_color", "theme_color"]) {
  if (typeof manifest[key] !== "string" || manifest[key].length === 0) problems.push(`missing ${key}`);
}
if (manifest.display !== "standalone") problems.push("display is not standalone");
const icons = Array.isArray(manifest.icons) ? manifest.icons : [];
for (const [size, purpose] of [["192x192", "any"], ["512x512", "any"], ["192x192", "maskable"], ["512x512", "maskable"]]) {
  if (!icons.some((icon) => icon.sizes === size && icon.purpose === purpose)) problems.push(`no ${purpose} ${size} icon`);
}
let checked = 0;
for (const icon of icons) {
  const file = `public${icon.src}`;
  if (!existsSync(file)) { problems.push(`missing ${file}`); continue; }
  const bytes = readFileSync(file);
  const size = `${bytes.readUInt32BE(16)}x${bytes.readUInt32BE(20)}`;
  if (bytes.toString("latin1", 1, 4) !== "PNG" || size !== icon.sizes) problems.push(`${file} is ${size}, declared ${icon.sizes}`);
  checked += 1;
}
if (problems.length > 0) { console.log(problems.join("; ")); process.exit(1); }
console.log(`${checked} icons present at their declared sizes`);
' 2>&1); then
  step "manifest" pass "$manifest_output"
else
  step "manifest" fail "$manifest_output"
fi

if grep -q "register(SERVICE_WORKER_URL" src/pwa/register.ts \
  && grep -q "SERVICE_WORKER_URL = '/sw.js'" src/pwa/caching.ts \
  && grep -q "registerServiceWorker(container" src/providers/PWAProvider.tsx; then
  step "service worker registered" pass "PWAProvider registers /sw.js through registerServiceWorker"
else
  step "service worker registered" fail "no client registration of /sw.js"
fi

third_party=$(grep -rli 'progressier' src/app src/lib src/proxy.ts public next.config.mjs 2>/dev/null || true)
if [ -z "$third_party" ]; then
  step "third-party PWA service" pass "no reference"
else
  step "third-party PWA service" fail "referenced in: $(printf '%s' "$third_party" | tr '\n' ' ')"
fi

vitest="$app_dir/node_modules/.bin/vitest"
if [ ! -x "$vitest" ]; then
  step "content security policy" fail "vitest is not installed in node_modules"
elif csp_output=$("$vitest" run src/lib/security/csp.test.ts 2>&1); then
  passed=$(printf '%s\n' "$csp_output" | sed -n 's/^ *Tests  *\([0-9][0-9]*\) passed.*/\1/p' | head -n 1)
  if [ -n "$passed" ]; then
    step "content security policy" pass "$passed tests passed against the release environment"
  else
    printf '%s\n' "$csp_output"
    step "content security policy" fail "no passing tests reported"
  fi
else
  printf '%s\n' "$csp_output"
  step "content security policy" fail "csp.test.ts exited non-zero"
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
