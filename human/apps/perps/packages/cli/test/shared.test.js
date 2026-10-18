const assert = require("node:assert/strict");
const { test } = require("node:test");
const { getApiErrorInfo } = require("../src/shared");

test("getApiErrorInfo reads structured nested error codes", () => {
  const info = getApiErrorInfo(
    { error: { code: "CONFLICT", message: "busy" } },
    409,
  );
  assert.equal(info.code, "CONFLICT");
  assert.equal(info.message, "busy");
});

test("getApiErrorInfo reads the flat top-level rate-limit shape", () => {
  const info = getApiErrorInfo(
    {
      error: "Marketplace rate limit exceeded",
      code: "MARKETPLACE_RATE_LIMITED",
      retryAfter: 30,
    },
    429,
  );
  assert.equal(info.code, "MARKETPLACE_RATE_LIMITED");
  assert.equal(info.message, "Marketplace rate limit exceeded");
});

test("getApiErrorInfo prefers a nested code over a flat one", () => {
  const info = getApiErrorInfo(
    { error: { code: "CONFLICT" }, code: "OTHER" },
    409,
  );
  assert.equal(info.code, "CONFLICT");
});

test("getApiErrorInfo trims codes and falls back to the HTTP status", () => {
  assert.equal(
    getApiErrorInfo({ code: "  RATE_LIMIT  " }, 429).code,
    "RATE_LIMIT",
  );
  const bare = getApiErrorInfo({}, 500);
  assert.equal(bare.code, null);
  assert.equal(bare.message, "HTTP 500");
});
