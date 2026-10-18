const assert = require("node:assert/strict");
const { test } = require("node:test");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { validateUpdatePayload } = require("../src/internal/marketplaceSchema");

const managedUrl = "/assets/marketplace/demo/cover.png";
function writeValidCoverImage(filePath, width = 1280, height = 800) {
  const png = Buffer.alloc(24);
  Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]).copy(png);
  png.writeUInt32BE(width, 16);
  png.writeUInt32BE(height, 20);
  fs.writeFileSync(filePath, png);
}

const response = (data, status = 200) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => data,
});

function fixture(t, command, options = {}) {
  const saved = new Map();
  function replace(relative, exports) {
    const id = require.resolve(relative);
    if (!saved.has(id)) saved.set(id, require.cache[id]);
    if (exports)
      require.cache[id] = { id, filename: id, loaded: true, exports };
    else delete require.cache[id];
  }
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "marketplace-test-"));
  writeValidCoverImage(path.join(dir, "cover.png"));
  const output = [];
  const calls = [];
  const selections = [];
  const manifest = {
    pluginId: "demo",
    npmName: "demo",
    coverImages: ["cover.png"],
    ...options.manifest,
  };
  const previousExit = process.exitCode;
  process.exitCode = 0;
  const shared = require("../src/shared");
  replace("../src/shared", {
    ...shared,
    ...Object.fromEntries(
      ["heading", "info", "warn", "error", "success"].map((key) => [
        key,
        (message) => output.push(message),
      ]),
    ),
    select: async (_message, choices) => {
      selections.push(choices);
      return choices[0].value;
    },
  });
  replace("../src/internal/auth", {
    requireAuth: () => {
      if (options.unauthenticated) process.exitCode = 1;
      return !options.unauthenticated;
    },
    isLoggedIn: () => options.loggedIn ?? false,
    getEmail: () => "test-user",
    saveOAuthToken: () => {},
    authenticatedFetch: async (url, init) => {
      calls.push({ url, init });
      if (options.fetch) return options.fetch(url, init);
      return url.endsWith("/cover-images")
        ? response({ url: managedUrl }, 201)
        : response({ id: "demo", status: "deprecated" });
    },
    fetchWithTimeout: async (url, init) => {
      calls.push({ url, init });
      if (options.fetch) return options.fetch(url, init);
      return response({
        plugins: [],
        pagination: { page: 1, limit: 20, total: 0, totalPages: 0 },
      });
    },
    getHttpTimeoutMs: () => 5000,
  });
  const manifestWrites = [];
  replace("../src/internal/manifest", {
    resolvePluginManifest: () => manifest,
    getRepoUrl: () => manifest.repoUrl ?? null,
    updateManifest: (dir, updates) => {
      manifestWrites.push({ dir, updates });
      return { ...manifest, ...updates };
    },
  });
  replace("../src/internal/coverUpload");
  // Re-bind the shared failure printer to this test's stubbed loggers.
  replace("../src/internal/marketplaceErrors");
  replace("../src/internal/paxeerNetworkSdkDocsMcpDetect", {
    maybePrintPaxeerNetworkDevEnvironmentHints: () => {},
  });
  replace(`../src/commands/${command}`);
  t.mock.method(console, "log", (message) => output.push(message));
  t.after(() => {
    for (const [id, entry] of saved) {
      if (entry) require.cache[id] = entry;
      else delete require.cache[id];
    }
    process.exitCode = previousExit;
    fs.rmSync(dir, { recursive: true, force: true });
  });
  return {
    dir,
    output,
    calls,
    selections,
    manifestWrites,
    replace,
    load: () => require(`../src/commands/${command}`),
  };
}

test("update dry-run validates cover-only metadata without network calls", async (t) => {
  const f = fixture(t, "update");
  t.mock.method(global, "fetch", () => {
    throw new Error("Unexpected real network");
  });
  await f.load().handler({ path: f.dir, "dry-run": true });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /cover\.png/);
  assert.match(f.output.join("\n"), /only on a real update/);
});

test("update validation mirrors name and description constraints", () => {
  const empty = validateUpdatePayload({ name: "   " });
  assert.match(empty.errors.join("\n"), /name must not be empty/);

  const tooLong = validateUpdatePayload({
    name: "x".repeat(121),
    description: "x".repeat(501),
  });
  assert.match(tooLong.errors.join("\n"), /name is too long/);
  assert.match(tooLong.errors.join("\n"), /description is too long/);

  const wrongType = validateUpdatePayload({ description: 123 });
  assert.match(wrongType.errors.join("\n"), /description must be a string/);
});

test("update dry-run rejects a non-image cover before network calls", async (t) => {
  const f = fixture(t, "update");
  fs.writeFileSync(path.join(f.dir, "cover.png"), "fixture");
  await f.load().handler({ path: f.dir, "dry-run": true });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /supported PNG, JPEG, or WebP/);
});

test("update rejects a non-image cover before uploading on a real run", async (t) => {
  const f = fixture(t, "update");
  fs.writeFileSync(path.join(f.dir, "cover.png"), "fixture");
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /supported PNG, JPEG, or WebP/);
});

test("update can clear usagePrompt with null", async (t) => {
  const f = fixture(t, "update", {
    manifest: { name: "Demo", usagePrompt: null, coverImages: [] },
  });
  await f.load().handler({ path: f.dir, "dry-run": true });
  assert.equal(process.exitCode, 0);
  assert.match(f.output.join("\n"), /"usagePrompt": null/);
});

test("update omits usagePrompt when the manifest never set it", async (t) => {
  const f = fixture(t, "update", {
    manifest: { name: "Demo", coverImages: [] },
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].init.method, "PATCH");
  assert.deepEqual(JSON.parse(f.calls[0].init.body), { name: "Demo" });
});

test("submission validation rejects an overlong npm name", () => {
  const { validateSubmission } = require("../src/internal/marketplaceSchema");
  const validation = validateSubmission({
    npmName: "a".repeat(215),
    repoUrl: "https://github.com/test/demo",
    name: "Demo",
    description: "Demo plugin",
    tags: ["UI"],
    coverImages: [managedUrl],
    usagePrompt: null,
  });
  assert.match(validation.errors.join("\n"), /npmName is too long/);
});

test("submission name and description limits apply after trimming", () => {
  const { validateSubmission } = require("../src/internal/marketplaceSchema");
  const base = {
    npmName: "demo",
    repoUrl: "https://github.com/test/demo",
    tags: ["UI"],
    coverImages: [managedUrl],
  };
  const padded = validateSubmission({
    ...base,
    name: `  ${"x".repeat(120)}  `,
    description: `  ${"x".repeat(500)}  `,
  });
  assert.equal(padded.valid, true);

  const tooLong = validateSubmission({
    ...base,
    name: "x".repeat(121),
    description: "x".repeat(501),
  });
  assert.match(tooLong.errors.join("\n"), /name is too long/);
  assert.match(tooLong.errors.join("\n"), /description is too long/);
});

test("submit dry-run omits unset optional fields from the payload preview", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      pluginId: null,
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: [managedUrl],
      usagePrompt: null,
    },
  });
  await f.load().handler({ path: f.dir, "dry-run": true });
  assert.equal(process.exitCode, 0);
  const output = f.output.join("\n");
  assert.match(output, /"coverImages":/);
  assert.doesNotMatch(output, /"pluginId":/);
  assert.doesNotMatch(output, /"usagePrompt":/);
});

test("submit dry-run rejects a non-image cover before network calls", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: ["cover.png"],
    },
  });
  fs.writeFileSync(path.join(f.dir, "cover.png"), "fixture");
  await f.load().handler({ path: f.dir, "dry-run": true });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /supported PNG, JPEG, or WebP/);
});

test("submit rejects a non-image cover before uploading on a real run", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: ["cover.png"],
    },
  });
  fs.writeFileSync(path.join(f.dir, "cover.png"), "fixture");
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /supported PNG, JPEG, or WebP/);
});

test("submit rejects a wrong-size cover before uploading on a real run", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: ["cover.png"],
    },
  });
  writeValidCoverImage(path.join(f.dir, "cover.png"), 999, 999);
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /must be exactly 1280 x 800 or 640 x 400/);
});

test("cover-only update uploads before PATCH and uses the returned URL", async (t) => {
  const f = fixture(t, "update");
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 2);
  assert.match(f.calls[0].url, /\/cover-images$/);
  assert.equal(f.calls[0].init.body.get("npmName"), "demo");
  assert.equal(f.calls[1].init.method, "PATCH");
  assert.deepEqual(JSON.parse(f.calls[1].init.body), {
    coverImages: [managedUrl],
  });
});

test("successful update persists uploaded cover URLs to the manifest", async (t) => {
  const f = fixture(t, "update");
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.deepEqual(f.manifestWrites, [
    { dir: f.dir, updates: { coverImages: [managedUrl] } },
  ]);
});

test("failed update keeps local cover paths in the manifest", async (t) => {
  const f = fixture(t, "update", {
    fetch: async (url) =>
      url.endsWith("/cover-images")
        ? response({ url: managedUrl }, 201)
        : response({ error: "rejected" }, 400),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.deepEqual(f.manifestWrites, []);
});

test("already-managed cover URLs are not rewritten", async (t) => {
  const f = fixture(t, "update", {
    manifest: { coverImages: [managedUrl] },
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].init.method, "PATCH");
  assert.deepEqual(f.manifestWrites, []);
});

for (const covers of [
  Array(6).fill("cover.png"),
  ["cover.png", "missing.png"],
  [""],
  [null],
  [123],
  ["."],
  "cover.png",
]) {
  test(`update rejects invalid cover sources before uploading: ${JSON.stringify(covers)}`, async (t) => {
    const f = fixture(t, "update", { manifest: { coverImages: covers } });
    await f.load().handler({ path: f.dir });
    assert.equal(process.exitCode, 1);
    assert.equal(f.calls.length, 0);
  });
}

test("update rejects unreadable local files without network calls", async (t) => {
  const f = fixture(t, "update");
  t.mock.method(fs, "accessSync", () => {
    throw new Error("EACCES");
  });
  await f.load().handler({ path: f.dir, "dry-run": true });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
});

test("update does not PATCH when cover upload fails", async (t) => {
  const f = fixture(t, "update", {
    fetch: async () => response({ error: "Upload rejected" }, 400),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 1);
  assert.match(f.calls[0].url, /\/cover-images$/);
  assert.match(f.output.join("\n"), /Upload rejected/);
});

for (const coverImages of [undefined, []]) {
  test(`update skips omitted/empty cover images: ${JSON.stringify(coverImages)}`, async (t) => {
    const f = fixture(t, "update", { manifest: { name: "Demo", coverImages } });
    await f.load().handler({ path: f.dir });
    assert.equal(f.calls.length, 1);
    assert.deepEqual(JSON.parse(f.calls[0].init.body), { name: "Demo" });
  });
}

test("disable directly PATCHes deprecated without listing plugins", async (t) => {
  const f = fixture(t, "disable");
  await f.load().handler({ pluginId: "demo" });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 1);
  assert.match(f.calls[0].url, /\/plugins\/demo$/);
  assert.equal(f.calls[0].init.method, "PATCH");
  assert.deepEqual(JSON.parse(f.calls[0].init.body), { status: "deprecated" });
});

test("disable selection only includes available owned plugins", async (t) => {
  const f = fixture(t, "disable", {
    fetch: async (url) =>
      response(
        url.endsWith("/my-plugins")
          ? {
              plugins: [
                { id: "available", status: "available" },
                { id: "pending", status: "under_review" },
                { id: "old", status: "deprecated" },
              ],
            }
          : { status: "deprecated" },
      ),
  });
  await f.load().handler({});
  assert.equal(f.selections[0].length, 1);
  assert.equal(f.selections[0][0].value, "available");
  assert.match(f.calls[1].url, /\/plugins\/available$/);
});

test("disable exits cleanly when no plugin can be delisted", async (t) => {
  const f = fixture(t, "disable", {
    fetch: async () =>
      response({ plugins: [{ id: "old", status: "deprecated" }] }),
  });
  await f.load().handler({});
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 1);
  assert.equal(f.selections.length, 0);
});

test("disable requires authentication before making requests", async (t) => {
  const f = fixture(t, "disable", { unauthenticated: true });
  await f.load().handler({ pluginId: "demo" });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
});

for (const status of [401, 403, 404, 409]) {
  test(`disable reports HTTP ${status}`, async (t) => {
    const f = fixture(t, "disable", {
      fetch: async () => response({ error: `failure-${status}` }, status),
    });
    await f.load().handler({ pluginId: "demo" });
    assert.equal(process.exitCode, 1);
    assert.match(f.output.join("\n"), new RegExp(`failure-${status}`));
  });
}

test("disable reports a failed plugin list without showing a selector", async (t) => {
  const f = fixture(t, "disable", {
    fetch: async () => response({ error: "Authentication expired" }, 401),
  });
  await f.load().handler({});
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 1);
  assert.equal(f.selections.length, 0);
  assert.match(f.output.join("\n"), /Authentication expired/);
});

test("disable handles network exceptions", async (t) => {
  const f = fixture(t, "disable", {
    fetch: async () => {
      throw new Error("offline");
    },
  });
  await f.load().handler({});
  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /offline/);
});

for (const port of [undefined, 9876, 9899, 9875, 9900, 9876.5, NaN, "abc"]) {
  test(`login validates port ${String(port)} before any side effects`, async (t) => {
    const f = fixture(t, "login");
    const started = [];
    const opened = [];
    f.replace("../src/internal/login-server", {
      startCallbackServer: ({ port: callbackPort }) => {
        started.push(callbackPort);
        const server = new EventEmitter();
        server.close = () => {};
        return {
          server,
          waitForToken: Promise.resolve({ token: "test" }),
          cancel: () => {},
          listen: () => Promise.resolve(callbackPort),
        };
      },
    });
    t.mock.method(
      require("node:child_process"),
      "exec",
      (command, callback) => {
        opened.push(command);
        callback(null);
      },
    );
    await f.load().handler({ port });
    const valid =
      port === undefined ||
      (Number.isInteger(port) && port >= 9876 && port <= 9899);
    assert.equal(process.exitCode, valid ? 0 : 1);
    assert.deepEqual(started, valid ? [port ?? 9876] : []);
    assert.equal(opened.length, valid ? 1 : 0);
  });
}

test("first submission with HTTP 202 reports acceptance, not replay", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response(
        { submission: { pluginIdCandidate: "demo", status: "queued" } },
        202,
      ),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.match(f.output.join("\n"), /Submission received, awaiting processing/);
  assert.match(f.output.join("\n"), /Status: queued/);
  assert.doesNotMatch(f.output.join("\n"), /idempotent replay/);
});

test("whoami reports an expired session instead of a valid login", async (t) => {
  const f = fixture(t, "whoami", {
    loggedIn: true,
    fetch: async () => response({ error: "Unauthorized" }, 401),
  });
  await f.load().handler({});
  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /no longer valid/);
});

test("whoami confirms a session the server still accepts", async (t) => {
  const f = fixture(t, "whoami", {
    loggedIn: true,
    fetch: async () => response({ submissions: [] }),
  });
  await f.load().handler({});
  assert.equal(process.exitCode, 0);
  assert.match(f.output.join("\n"), /Session is valid/);
});

test("whoami does not probe the server when no credentials are stored", async (t) => {
  const f = fixture(t, "whoami");
  await f.load().handler({});
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
});

test("whoami tolerates an unreachable marketplace", async (t) => {
  const f = fixture(t, "whoami", {
    loggedIn: true,
    fetch: async () => {
      throw new Error("offline");
    },
  });
  await f.load().handler({});
  assert.equal(process.exitCode, 0);
  assert.match(f.output.join("\n"), /Could not reach the marketplace/);
});

test("submit persists uploaded cover URLs after acceptance", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: ["cover.png"],
    },
    fetch: async (url) =>
      url.endsWith("/cover-images")
        ? response({ url: managedUrl }, 201)
        : response({ submission: { id: "sub-1", status: "queued" } }, 202),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.match(f.output.join("\n"), /Submission ID: sub-1/);
  assert.deepEqual(f.manifestWrites, [
    { dir: f.dir, updates: { coverImages: [managedUrl] } },
  ]);
});

test("submit rejected by the server keeps local cover paths", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: ["cover.png"],
    },
    fetch: async (url) =>
      url.endsWith("/cover-images")
        ? response({ url: managedUrl }, 201)
        : response({ error: "Connect a GitHub account" }, 409),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.deepEqual(f.manifestWrites, []);
});

test("submit 409 with an active submission tells the user to wait", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response(
        {
          error: "An active submission already exists for this plugin ID",
          submissionId: "sub-9",
        },
        409,
      ),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  const output = f.output.join("\n");
  assert.match(output, /still being processed/);
  assert.match(output, /Submission ID: sub-9/);
  assert.doesNotMatch(output, /Try a different pluginId/);
});

test("submit 409 for an already-registered package points at update", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response({ error: "This npm package is already registered" }, 409),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  const output = f.output.join("\n");
  assert.match(output, /already registered/);
  assert.match(output, /paxeer-network-devkit update/);
  assert.doesNotMatch(output, /Try a different pluginId/);
});

test("submit 409 for a taken pluginId keeps the pluginId advice", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response(
        {
          error:
            'The plugin ID "demo" is already registered by the listing for other-pkg',
        },
        409,
      ),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /different pluginId/);
});

test("submit 429 surfaces the retry window", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response(
        {
          error: "Marketplace rate limit exceeded",
          code: "MARKETPLACE_RATE_LIMITED",
          retryAfter: 30,
        },
        429,
      ),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /Rate limit exceeded/);
  assert.match(f.output.join("\n"), /30 seconds/);
});

test("cover upload 429 includes the retry window and stops the run", async (t) => {
  const f = fixture(t, "submit", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      repoUrl: "https://github.com/test/demo",
      tags: ["UI"],
      coverImages: ["cover.png"],
    },
    fetch: async (url) =>
      url.endsWith("/cover-images")
        ? response(
            { error: "Marketplace rate limit exceeded", retryAfter: 45 },
            429,
          )
        : response({ submission: { status: "queued" } }, 202),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 1);
  assert.match(f.calls[0].url, /\/cover-images$/);
  assert.match(f.output.join("\n"), /retry after 45s/);
});

for (const command of ["submit", "update"]) {
  for (const dryRun of [true, false]) {
    for (const url of [
      "https://example.com/cover.png",
      "https://",
      "/assets/other/cover.png",
      "/assets.evil/marketplace/cover.png",
      "/assets/marketplace/../other/cover.png",
      "/assets/marketplace/cover.png",
    ]) {
      test(`${command} rejects ${url} before uploading (dry-run=${dryRun})`, async (t) => {
        const f = fixture(t, command, {
          manifest: {
            name: "Demo",
            description: "Demo plugin",
            repoUrl: "https://github.com/test/demo",
            tags: ["UI"],
            coverImages: ["cover.png", url],
          },
        });
        await f.load().handler({ path: f.dir, "dry-run": dryRun });
        assert.equal(process.exitCode, 1);
        assert.equal(f.calls.length, 0);
        assert.equal(f.manifestWrites.length, 0);
        assert.match(f.output.join("\n"), /Invalid cover URL/);
      });
    }
  }
}

test("update reuses valid managed covers without re-uploading", async (t) => {
  const f = fixture(t, "update", { manifest: { coverImages: [managedUrl] } });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].init.method, "PATCH");
  assert.deepEqual(JSON.parse(f.calls[0].init.body).coverImages, [managedUrl]);
});

test("cover validation accepts only the configured emulator origin and marketplace path", async (t) => {
  const previous = process.env.GCS_PUBLIC_BASE_URL;
  process.env.GCS_PUBLIC_BASE_URL = " http://localhost:4443/// ";
  t.after(() => {
    if (previous === undefined) delete process.env.GCS_PUBLIC_BASE_URL;
    else process.env.GCS_PUBLIC_BASE_URL = previous;
  });
  const { validateCoverSources } = require("../src/internal/coverUpload");
  const validate = (url) =>
    validateCoverSources({ pluginDir: process.cwd(), coverSources: [url] });
  assert.doesNotThrow(() =>
    validate("http://localhost:4443/marketplace/cover.png"),
  );
  assert.doesNotThrow(() => validate(managedUrl));
  assert.throws(
    () => validate("http://localhost:4444/marketplace/cover.png"),
    /Invalid cover URL/,
  );
  assert.throws(
    () => validate("http://localhost:4443/other/cover.png"),
    /Invalid cover URL/,
  );
  delete process.env.GCS_PUBLIC_BASE_URL;
  assert.throws(
    () => validate("http://localhost:4443/marketplace/cover.png"),
    /Invalid cover URL/,
  );
});

test("generateManifest omits usagePrompt instead of defaulting it to null", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "marketplace-manifest-"));
  try {
    fs.writeFileSync(
      path.join(dir, "package.json"),
      JSON.stringify({ name: "demo", description: "Demo" }),
    );
    const { generateManifest } = require("../src/internal/manifest");

    generateManifest(dir, { pluginId: "demo", name: "Demo" });
    const stored = JSON.parse(
      fs.readFileSync(path.join(dir, ".paxeer-network-manifest.json"), "utf-8"),
    );
    assert.ok(!("usagePrompt" in stored));

    generateManifest(dir, { pluginId: "demo", usagePrompt: "How to use" });
    const storedWithPrompt = JSON.parse(
      fs.readFileSync(path.join(dir, ".paxeer-network-manifest.json"), "utf-8"),
    );
    assert.equal(storedWithPrompt.usagePrompt, "How to use");
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("search maps display-name tags to backend slugs", async (t) => {
  const f = fixture(t, "search");
  await f.load().handler({
    tag: "Order Entry, trading",
    sort: "downloads",
    order: "desc",
    page: 1,
    limit: 20,
  });
  assert.equal(process.exitCode, 0);
  assert.equal(f.calls.length, 1);
  assert.match(f.calls[0].url, /tag=order-entry%2Ctrading/);
});

test("search passes raw tag slugs through unchanged", async (t) => {
  const f = fixture(t, "search");
  await f.load().handler({
    tag: "orderbook",
    sort: "downloads",
    order: "desc",
    page: 1,
    limit: 20,
  });
  assert.equal(process.exitCode, 0);
  assert.match(f.calls[0].url, /tag=orderbook/);
});

test("search drops a tag that is empty after trimming", async (t) => {
  const f = fixture(t, "search");
  await f.load().handler({
    tag: "  ",
    sort: "downloads",
    order: "desc",
    page: 1,
    limit: 20,
  });
  assert.equal(process.exitCode, 0);
  assert.doesNotMatch(f.calls[0].url, /tag=/);
});

test("CLI startup loads commands without executing co-located test files", () => {
  const { spawnSync } = require("node:child_process");
  // A command that awaits the network keeps the event loop alive, which is
  // what lets a wrongly-required *.test.js run its suite inside the CLI
  // process (fast-exiting commands like --help exit before that happens).
  // Verified by mutation: without commandDir's exclude filter this leaks
  // hundreds of test-runner lines into the command output.
  const result = spawnSync(
    process.execPath,
    [path.join(__dirname, "../bin/cli.js"), "search", "zzz"],
    {
      encoding: "utf-8",
      env: {
        ...process.env,
        PAXEER_NETWORK_API_URL: "http://127.0.0.1:9",
        PAXEER_NETWORK_HTTP_TIMEOUT_MS: "1000",
      },
      timeout: 30000,
    },
  );
  const output = `${result.stdout || ""}${result.stderr || ""}`;
  // Sanity: the child really reached the search command (a broken bin would
  // fail this match instead of passing vacuously).
  assert.match(output, /Available Marketplace Plugins/);
  assert.doesNotMatch(output, /# Subtest|✔|^ok \d+/m);
});

test("update 409 with an active submission tells the user to wait", async (t) => {
  const f = fixture(t, "update", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response(
        {
          error: "An active submission already exists for this plugin ID",
          submissionId: "sub-7",
        },
        409,
      ),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  const output = f.output.join("\n");
  assert.match(output, /still being processed/);
  assert.match(output, /Submission ID: sub-7/);
  assert.doesNotMatch(output, /Try a different pluginId/);
});

test("update 429 surfaces the retry window and error code", async (t) => {
  const f = fixture(t, "update", {
    manifest: {
      name: "Demo",
      description: "Demo plugin",
      coverImages: [managedUrl],
    },
    fetch: async () =>
      response(
        {
          error: "Marketplace rate limit exceeded",
          code: "MARKETPLACE_RATE_LIMITED",
          retryAfter: 30,
        },
        429,
      ),
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  const output = f.output.join("\n");
  assert.match(output, /Rate limit exceeded/);
  assert.match(output, /30 seconds/);
  assert.match(output, /Error code: MARKETPLACE_RATE_LIMITED/);
});

test("update requires npmName before uploading local covers", async (t) => {
  const f = fixture(t, "update", {
    manifest: {
      npmName: null,
      name: "Demo",
      description: "Demo plugin",
      coverImages: ["cover.png"],
    },
  });
  await f.load().handler({ path: f.dir });
  assert.equal(process.exitCode, 1);
  assert.equal(f.calls.length, 0);
  assert.match(f.output.join("\n"), /npmName is required/);
});
