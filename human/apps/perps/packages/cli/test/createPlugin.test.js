const assert = require("node:assert/strict");
const { test } = require("node:test");
const path = require("node:path");

function createPluginFixture(t, options = {}) {
  const saved = new Map();
  function replace(relative, exports) {
    const id = require.resolve(relative);
    if (!saved.has(id)) saved.set(id, require.cache[id]);
    if (exports)
      require.cache[id] = { id, filename: id, loaded: true, exports };
    else delete require.cache[id];
  }

  const output = [];
  const prompts = [];
  const generateCalls = [];

  const shared = require("../src/shared");
  replace("../src/shared", {
    ...shared,
    ...Object.fromEntries(
      ["heading", "info", "warn", "error", "success"].map((key) => [
        key,
        (message) => output.push(message),
      ]),
    ),
    input: async (message) => {
      prompts.push(message);
      return "prompted-value";
    },
    select: async (message) => {
      prompts.push(message);
      return "Trading.Layout.Desktop";
    },
    confirm: async (message) => {
      prompts.push(message);
      return false;
    },
  });

  const templateGenerator = require("../src/internal/templateGenerator");
  replace("../src/internal/templateGenerator", {
    ...templateGenerator,
    generateFromTemplate: async (config) => {
      generateCalls.push(config);
      if (options.generateError) {
        throw new Error(options.generateError);
      }
    },
  });
  replace("../src/internal/manifest", {
    generateManifest: () => ({}),
  });
  replace("../src/internal/paxeerNetworkSdkDocsMcpDetect", {
    maybePrintPaxeerNetworkDevEnvironmentHints: () => {},
  });
  replace("../src/commands/create/plugin");

  const previousExit = process.exitCode;
  process.exitCode = 0;
  t.mock.method(console, "log", (message) => output.push(String(message)));
  t.mock.method(console, "error", () => {});
  t.after(() => {
    for (const [id, entry] of saved) {
      if (entry) require.cache[id] = entry;
      else delete require.cache[id];
    }
    process.exitCode = previousExit;
  });

  return {
    output,
    prompts,
    generateCalls,
    load: () => require("../src/commands/create/plugin"),
  };
}

test("--no-interactive (interactive: false) skips every prompt and derives id/target from --name", async (t) => {
  const f = createPluginFixture(t);
  await f.load().handler({
    interactive: false,
    name: "Foo",
    interceptor: "Trading.Layout.Desktop",
    "skip-install": true,
  });

  assert.deepEqual(f.prompts, []);
  assert.equal(f.generateCalls.length, 1);
  assert.equal(f.generateCalls[0].targetDir, path.resolve("./Foo"));
  assert.equal(f.generateCalls[0].vars.pluginId, "foo");
  assert.equal(f.generateCalls[0].vars.name, "Foo");
  assert.equal(process.exitCode, 0);
  assert.match(f.output.join("\n"), /Plugin created successfully/);
});

test("--no-interactive without --name fails instead of prompting", async (t) => {
  const f = createPluginFixture(t);
  await f
    .load()
    .handler({ interactive: false, interceptor: "Trading.Layout.Desktop" });

  assert.deepEqual(f.prompts, []);
  assert.equal(f.generateCalls.length, 0);
  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /Missing required option: --name/);
});

test("--no-interactive without --interceptor fails instead of prompting", async (t) => {
  const f = createPluginFixture(t);
  await f.load().handler({ interactive: false, name: "Foo" });

  assert.deepEqual(f.prompts, []);
  assert.equal(f.generateCalls.length, 0);
  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /Missing required option: --interceptor/);
});

test("interactive mode still prompts when the flag is absent", async (t) => {
  const f = createPluginFixture(t);
  await f.load().handler({});

  assert.ok(f.prompts.some((message) => /plugin name/i.test(message)));
  assert.equal(f.generateCalls.length, 0);
});

test("template generation failure exits with a non-zero code", async (t) => {
  const f = createPluginFixture(t, {
    generateError: "Target directory is not empty",
  });
  await f.load().handler({
    name: "Foo",
    id: "foo",
    interceptor: "Trading.Layout.Desktop",
    target: "./Foo",
    "skip-install": true,
  });

  assert.equal(process.exitCode, 1);
  assert.match(f.output.join("\n"), /Failed to create plugin/);
});
