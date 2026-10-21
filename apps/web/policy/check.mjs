import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";

import { appContentSecurityPolicy } from "../src/security/csp.ts";

const root = new URL("../", import.meta.url);
const [manifest, lockfile, budget] = await Promise.all(
  ["package.json", "package-lock.json", "policy/runtime-dependencies.json"].map(async (path) =>
    JSON.parse(await readFile(new URL(path, root), "utf8")),
  ),
);

assert.equal(lockfile.lockfileVersion, 3, "the web lockfile must use npm lockfile version 3");
assert.deepEqual(
  manifest.dependencies,
  budget.approved,
  "a direct runtime dependency changed without dependency-budget approval",
);
assert.ok(
  Object.keys(manifest.dependencies).length <= budget.maximumDirectRuntimeDependencies,
  "the direct runtime dependency budget was exceeded",
);
assert.deepEqual(
  lockfile.packages[""].dependencies,
  manifest.dependencies,
  "the web lockfile does not pin the declared runtime dependencies",
);

const allowedLicenses = new Set([
  "0BSD",
  "Apache-2.0",
  "Apache-2.0 AND LGPL-3.0-or-later",
  "Apache-2.0 AND LGPL-3.0-or-later AND MIT",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "BlueOak-1.0.0",
  "CC-BY-4.0",
  "ISC",
  "LGPL-3.0-or-later",
  "MIT",
  "MPL-2.0",
]);

for (const [path, metadata] of Object.entries(lockfile.packages)) {
  if (path === "") {
    continue;
  }
  let pinned = metadata;
  if (metadata.link === true) {
    assert.equal(path, "node_modules/@layerx/ui", `${path} is an unapproved local package link`);
    assert.equal(metadata.resolved, "packages/layerx-ui", "@layerx/ui must resolve to its checked-in package");
    pinned = lockfile.packages[metadata.resolved];
    assert.equal(pinned.name, "@layerx/ui", "the local package identity changed");
    assert.equal(pinned.version, "0.1.0", "the local package version changed");
  }
  assert.equal(typeof pinned.version, "string", `${path} is not pinned to a version`);
  assert.ok(allowedLicenses.has(pinned.license), `${path} uses non-allowlisted license ${pinned.license}`);
}

const productionPolicy = appContentSecurityPolicy("abcdefghijklmnop123456", false);
assert.match(productionPolicy, /default-src 'self'/u, "the app CSP must default to first-party content");
assert.match(productionPolicy, /script-src 'self' 'nonce-[^']+' 'strict-dynamic'/u, "the app CSP must nonce scripts");
assert.match(productionPolicy, /style-src 'self' 'nonce-[^']+'/u, "the app CSP must nonce styles");
assert.match(productionPolicy, /connect-src 'self'/u, "authenticated connections must remain first-party");
assert.match(productionPolicy, /font-src 'self'/u, "font origins must be pinned");
assert.doesNotMatch(productionPolicy, /https?:/u, "the app CSP must not admit third-party origins");
assert.doesNotMatch(productionPolicy, /unsafe-inline|unsafe-eval/u, "the production app CSP must remain strict");

async function sourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map(async (entry) => {
      const location = new URL(entry.name, directory);
      if (entry.isDirectory()) {
        return sourceFiles(new URL(`${entry.name}/`, directory));
      }
      return /\.[cm]?[jt]sx?$/u.test(entry.name) ? [location] : [];
    }),
  );
  return nested.flat();
}

const authenticatedRoots = [new URL("../src/", import.meta.url)];
for (const authenticatedRoot of authenticatedRoots) {
  for (const file of await sourceFiles(authenticatedRoot)) {
    const source = await readFile(file, "utf8");
    assert.doesNotMatch(source, /from\s+["']next\/script["']/u, `${file.pathname} imports next/script`);
    assert.doesNotMatch(source, /<script\b/u, `${file.pathname} contains a raw script element`);
    assert.doesNotMatch(source, /src\s*=\s*["']https?:\/\//u, `${file.pathname} loads a third-party script`);
  }
}

console.log("web dependency, lockfile, license, runtime-budget, and authenticated-plane policies passed");
