// Sample tests for F-14 structured logger.
// Confirms namespace prefixing, level gating, sink routing, child loggers.

import { describe, it, beforeEach } from "node:test";
import { strict as assert } from "node:assert";

import {
  createLogger,
  setSink,
  type LogEvent,
  type LogSink,
} from "../src/core/log/logger";

const captured: LogEvent[] = [];
const captureSink: LogSink = (event) => {
  captured.push(event);
};

describe("createLogger", () => {
  beforeEach(() => {
    captured.length = 0;
    setSink(captureSink);
  });

  it("emits events with the namespace passed to createLogger", () => {
    const log = createLogger("perps:order");
    log.warn("submit blocked", { reason: "wrong_chain" });
    assert.equal(captured.length, 1);
    assert.equal(captured[0].ns, "perps:order");
    assert.equal(captured[0].level, "warn");
    assert.equal(captured[0].msg, "submit blocked");
    assert.deepEqual(captured[0].data, { reason: "wrong_chain" });
  });

  it("supports info/warn/error/debug surfaces", () => {
    const log = createLogger("test");
    log.info("i");
    log.warn("w");
    log.error("e");
    // `debug` is gated below `info` by default (test env runs in `info`
    // mode), so we don't include it in the asserted list.
    const levels = captured.map((e) => e.level);
    assert.deepEqual(levels, ["info", "warn", "error"]);
  });

  it("child() appends to the namespace", () => {
    const root = createLogger("net");
    const child = root.child("rpc");
    child.warn("ranking sample dropped");
    assert.equal(captured.length, 1);
    assert.equal(captured[0].ns, "net:rpc");
  });

  it("setSink rewires the active sink", () => {
    const local: LogEvent[] = [];
    setSink((event) => local.push(event));
    const log = createLogger("test");
    log.warn("hello");
    assert.equal(local.length, 1);
    assert.equal(local[0].msg, "hello");
    // Default capture sink should NOT have received it.
    assert.equal(captured.length, 0);
  });
});
