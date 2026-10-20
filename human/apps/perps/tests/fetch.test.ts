// Sample tests for F-07 shared HTTP policy.
// Confirms retry behavior, timeout, and error taxonomy for fetchJsonWithPolicy.

import { describe, it, beforeEach, afterEach } from "node:test";
import { strict as assert } from "node:assert";

import {
  fetchJsonWithPolicy,
  fetchWithPolicy,
  retryAfterMs,
  FetchPolicyError,
} from "../src/core/net/fetch";

const originalFetch = globalThis.fetch;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

describe("fetchJsonWithPolicy", () => {
  let calls = 0;
  beforeEach(() => {
    calls = 0;
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("never retries a mutation", async () => {
    globalThis.fetch = async () => { calls++; return new Response("bad", { status: 503 }); };
    await assert.rejects(fetchJsonWithPolicy("https://example.test", { method: "POST", retries: 3 }));
    assert.equal(calls, 1);
  });
  it("honors Retry-After seconds/date and refuses to retry early for long delays", async () => {
    assert.equal(retryAfterMs("2", 0), 2000);
    assert.equal(retryAfterMs("Thu, 01 Jan 1970 00:00:03 GMT", 0), 3000);
    globalThis.fetch = async () => { calls++; return new Response("", { status: 429, headers: { "Retry-After": "120" } }); };
    await assert.rejects(fetchWithPolicy("https://example.test", { retries: 3 }));
    assert.equal(calls, 1);
  });
  it("normalizes malformed JSON for all transport callers", async () => {
    globalThis.fetch = async () => new Response("<html>");
    const response = await fetchWithPolicy("https://example.test");
    await assert.rejects(response.json(), (e: unknown) => e instanceof FetchPolicyError && e.kind === "parse");
  });
  it("caller cancellation interrupts retry sleep", async () => {
    const controller = new AbortController();
    globalThis.fetch = async () => { calls++; return new Response("", { status: 429, headers: { "Retry-After": "30" } }); };
    const request = fetchWithPolicy("https://example.test", { signal: controller.signal });
    setTimeout(() => controller.abort(), 10);
    await assert.rejects(request, (e: unknown) => e instanceof FetchPolicyError && e.kind === "abort");
    assert.equal(calls, 1);
  });
  it("returns parsed JSON on a 200", async () => {
    globalThis.fetch = async () => {
      calls += 1;
      return jsonResponse({ ok: true });
    };
    const res = await fetchJsonWithPolicy<{ ok: boolean }>(
      "https://example.test/x",
    );
    assert.equal(calls, 1);
    assert.deepEqual(res, { ok: true });
  });

  it("does not retry 4xx (except 408/425/429)", async () => {
    globalThis.fetch = async () => {
      calls += 1;
      return new Response("nope", { status: 400 });
    };
    await assert.rejects(
      fetchJsonWithPolicy("https://example.test/x", { retries: 3 }),
      (err: unknown) => {
        if (!(err instanceof FetchPolicyError)) return false;
        assert.equal(err.kind, "http");
        assert.equal(err.status, 400);
        return true;
      },
    );
    assert.equal(calls, 1);
  });

  it("retries 5xx up to the configured budget", async () => {
    globalThis.fetch = async () => {
      calls += 1;
      return new Response("boom", { status: 502 });
    };
    await assert.rejects(
      fetchJsonWithPolicy("https://example.test/x", {
        retries: 2,
        retryBaseMs: 1,
      }),
      (err: unknown) => err instanceof FetchPolicyError && err.kind === "http",
    );
    // Initial + 2 retries.
    assert.equal(calls, 3);
  });

  it("retries 429 with backoff and eventually succeeds", async () => {
    globalThis.fetch = async () => {
      calls += 1;
      if (calls < 3) return new Response("slow", { status: 429 });
      return jsonResponse({ ok: 1 });
    };
    const res = await fetchJsonWithPolicy<{ ok: number }>(
      "https://example.test/x",
      { retries: 5, retryBaseMs: 1 },
    );
    assert.equal(calls, 3);
    assert.deepEqual(res, { ok: 1 });
  });

  it("times out and surfaces a typed FetchPolicyError", async () => {
    globalThis.fetch = (_url, init) =>
      new Promise<Response>((_, reject) => {
        // Reject with a DOMException(name=AbortError) when caller aborts —
        // matches how the platform's `fetch` rejects on signal abort. The
        // policy distinguishes timeout from network error via this exact
        // shape, so the test must mirror it.
        const sig = init?.signal as AbortSignal | undefined;
        if (sig) {
          sig.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        }
      });
    await assert.rejects(
      fetchJsonWithPolicy("https://example.test/x", {
        retries: 0,
        timeoutMs: 25,
      }),
      (err: unknown) => err instanceof FetchPolicyError && err.kind === "timeout",
    );
  });
});
