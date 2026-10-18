const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  normalizePlugins,
  truncate,
  stripAnsi,
  visibleLength,
  terminalLink,
  parseGithubRepoUrl,
  renderTable,
} = require("../src/internal/formatting");

describe("normalizePlugins", () => {
  it("unwraps common API response shapes", () => {
    assert.deepEqual(normalizePlugins([{ id: 1 }]), [{ id: 1 }]);
    assert.deepEqual(normalizePlugins({ data: [{ id: 2 }] }), [{ id: 2 }]);
    assert.deepEqual(normalizePlugins({ plugins: [{ id: 3 }] }), [{ id: 3 }]);
    assert.deepEqual(normalizePlugins({ data: { items: [{ id: 4 }] } }), [
      { id: 4 },
    ]);
    assert.deepEqual(normalizePlugins({}), []);
  });
});

describe("truncate", () => {
  it("shortens long strings with ellipsis", () => {
    assert.equal(truncate("hello world", 8), "hello...");
    assert.equal(truncate("short", 10), "short");
  });
});

describe("stripAnsi / visibleLength", () => {
  it("ignores ANSI codes when measuring width", () => {
    const styled = "\x1b[31mred\x1b[0m";
    assert.equal(stripAnsi(styled), "red");
    assert.equal(visibleLength(styled), 3);
  });
});

describe("terminalLink", () => {
  it("returns plain label when not a TTY", () => {
    const originalIsTTY = process.stdout.isTTY;
    process.stdout.isTTY = false;
    try {
      assert.equal(terminalLink("pkg", "https://example.com"), "pkg");
    } finally {
      process.stdout.isTTY = originalIsTTY;
    }
  });
});

describe("parseGithubRepoUrl", () => {
  it("parses https and ssh GitHub URLs", () => {
    assert.deepEqual(parseGithubRepoUrl("https://github.com/o/r.git"), {
      label: "o/r",
      url: "https://github.com/o/r",
    });
    assert.deepEqual(parseGithubRepoUrl("git@github.com:o/r.git"), {
      label: "o/r",
      url: "https://github.com/o/r",
    });
    assert.equal(parseGithubRepoUrl(""), null);
  });
});

describe("renderTable", () => {
  it("aligns columns by visible width", () => {
    const output = renderTable([{ id: "1", name: "alpha" }], {
      headers: ["ID", "Name"],
      keys: ["id", "name"],
    });
    assert.match(output, /ID\s+Name/);
    assert.match(output, /1\s+alpha/);
  });
});
