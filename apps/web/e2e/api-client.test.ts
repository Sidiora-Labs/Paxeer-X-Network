import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import test from "node:test";

import { conformance, type ConformanceRun } from "../src/api/generated/conformance.ts";
import {
  HumanApiError,
  createHumanApiClient,
  decodeMoney,
  encodeMoney,
  operationNames,
  operations,
  type HumanApiClient,
  type JsonValue,
  type OperationShape,
} from "../src/api/generated/index.ts";

const goldenRoot = new URL("../../../services/human/schema/human-api/golden/", import.meta.url);

interface GoldenRequest {
  method: string;
  path: string;
  headers?: Record<string, string>;
  body?: JsonValue;
}

interface GoldenError {
  code: string;
  copy_key: string;
  retry: string;
  retry_after_ms?: number;
  field?: string;
}

interface GoldenEnvelope {
  ok: boolean;
  result?: JsonValue;
  error?: GoldenError;
  trace: string;
}

interface GoldenResponse {
  status: number;
  body: GoldenEnvelope;
}

interface RecordedRequest {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: string;
}

async function readGolden<Shape>(name: string): Promise<Shape> {
  return JSON.parse(await readFile(new URL(name, goldenRoot), "utf8")) as Shape;
}

function templateParams(template: string, actual: string): Record<string, string> {
  const expected = template.split("/");
  const found = actual.split("/");
  assert.equal(found.length, expected.length, "the golden path " + actual + " must match the template " + template);
  const params: Record<string, string> = {};
  expected.forEach((segment, index) => {
    const value = found[index] ?? "";
    if (segment.startsWith("{") && segment.endsWith("}")) {
      params[segment.slice(1, -1)] = decodeURIComponent(value);
    } else {
      assert.equal(value, segment, "the golden path " + actual + " must match the template " + template);
    }
  });
  return params;
}

function goldenIdempotencyKey(request: GoldenRequest): string | undefined {
  for (const [name, value] of Object.entries(request.headers ?? {})) {
    if (name.toLowerCase() === "idempotency-key") {
      return value;
    }
  }
  return undefined;
}

function buildRun(client: HumanApiClient, name: string, shape: OperationShape, request: GoldenRequest): ConformanceRun {
  const run: {
    client: HumanApiClient;
    params: Record<string, string>;
    body?: JsonValue;
    idempotencyKey?: string;
  } = { client, params: templateParams(shape.path, request.path) };
  if (request.body !== undefined) {
    run.body = request.body;
  }
  if (shape.idempotency) {
    const key = goldenIdempotencyKey(request);
    assert.ok(key !== undefined, name + ": an idempotent operation's golden request must carry the Idempotency-Key header");
    run.idempotencyKey = key;
  }
  return run;
}

test("every operation ships golden vectors and every vector belongs to a generated operation", async () => {
  const files = await readdir(goldenRoot);
  for (const name of operationNames) {
    for (const kind of ["request", "response", "failure"]) {
      assert.ok(files.includes(name + "." + kind + ".json"), name + " must ship a golden " + kind + " vector");
    }
  }
  for (const file of files) {
    const stem = file.replace(/\.(?:request|response|failure)\.json$/, "");
    assert.notEqual(stem, file, file + " does not follow the golden vector convention");
    assert.ok((operationNames as readonly string[]).includes(stem), file + " does not belong to a generated operation");
  }
});

test("the generated client round-trips every golden vector over real HTTP", async () => {
  let respondStatus = 200;
  let respondBody = "";
  const recordedRequests: RecordedRequest[] = [];
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => {
      chunks.push(chunk);
    });
    request.on("end", () => {
      const headers: Record<string, string> = {};
      for (const [name, value] of Object.entries(request.headers)) {
        if (typeof value === "string") {
          headers[name] = value;
        }
      }
      recordedRequests.push({
        method: request.method ?? "",
        path: request.url ?? "",
        headers,
        body: Buffer.concat(chunks).toString("utf8"),
      });
      response.statusCode = respondStatus;
      response.setHeader("content-type", "application/json");
      response.end(respondBody);
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const address = server.address() as AddressInfo;
    const client = createHumanApiClient({ baseUrl: "http://127.0.0.1:" + String(address.port) });
    for (const name of operationNames) {
      const shape = operations[name];
      const request = await readGolden<GoldenRequest>(name + ".request.json");
      const response = await readGolden<GoldenResponse>(name + ".response.json");
      const failure = await readGolden<GoldenResponse>(name + ".failure.json");
      respondStatus = response.status;
      respondBody = JSON.stringify(response.body);
      recordedRequests.length = 0;
      const result = await conformance[name](buildRun(client, name, shape, request));
      const observed = recordedRequests[0];
      assert.ok(observed !== undefined, name + ": the client must reach the service over HTTP");
      assert.equal(recordedRequests.length, 1, name + ": the client must make exactly one request");
      assert.equal(observed.method, request.method, name + ": the client must send the golden method");
      assert.equal(observed.path, request.path, name + ": the client must send the golden path");
      for (const [header, value] of Object.entries(request.headers ?? {})) {
        assert.equal(observed.headers[header.toLowerCase()], value, name + ": the client must send the golden " + header + " header");
      }
      if (request.body === undefined) {
        assert.equal(observed.body, "", name + ": a bodyless operation must send no request body");
      } else {
        assert.deepEqual(JSON.parse(observed.body) as JsonValue, request.body, name + ": decoding then re-encoding the golden request body must be the identity");
      }
      assert.deepEqual(result, response.body.result, name + ": decoding then re-encoding the golden response result must be the identity");
      respondStatus = failure.status;
      respondBody = JSON.stringify(failure.body);
      await assert.rejects(
        conformance[name](buildRun(client, name, shape, request)),
        (error: unknown) => {
          assert.ok(error instanceof HumanApiError, name + ": a failure envelope must surface as HumanApiError");
          assert.equal(error.status, failure.status, name + ": the error must carry the failure status");
          assert.equal(error.trace, failure.body.trace, name + ": the error must carry the envelope trace");
          const goldenError = failure.body.error;
          assert.ok(goldenError !== undefined, name + ": the golden failure must carry a structured error");
          assert.equal(error.detail.code, goldenError.code, name + ": the error must carry the stable machine code");
          assert.equal(error.detail.copy_key, goldenError.copy_key, name + ": the error must carry the copy-catalog key");
          assert.equal(error.detail.retry, goldenError.retry, name + ": the error must carry the retriability classification");
          if (goldenError.retry_after_ms !== undefined) {
            assert.equal(error.detail.retry_after_ms, goldenError.retry_after_ms, name + ": the error must carry the retry timing");
          }
          return true;
        },
      );
    }
  } finally {
    await new Promise<void>((resolve) => {
      server.close(() => {
        resolve();
      });
    });
  }
});

test("amounts decode to bigint and re-encode to the exact decimal string", () => {
  const money = decodeMoney({ amount: "250000000", currency: "LXP" }, "money");
  assert.equal(money.amount, 250000000n);
  assert.deepEqual(encodeMoney(money), { amount: "250000000", currency: "LXP" });
});

const EXPLORER_TRANSPORT = "explorer/client.ts";

test("the web application reaches the service only through the generated client", async () => {
  const srcRoot = new URL("../src/", import.meta.url);
  const files = await readdir(srcRoot, { recursive: true });
  for (const entry of files) {
    const relative = entry.replaceAll("\\", "/");
    if (!/\.(?:ts|tsx)$/.test(relative) || relative.startsWith("api/")) {
      continue;
    }
    const body = await readFile(new URL(relative, srcRoot), "utf8");
    assert.ok(!body.includes("api/generated"), "src/" + relative + " must consume src/api instead of the generated client directly");
    if (relative !== EXPLORER_TRANSPORT) {
      assert.ok(!body.includes("explorerOrigin"), "src/" + relative + " must reach the explorer read API through src/" + EXPLORER_TRANSPORT);
      for (const call of body.matchAll(/\bfetch\s*\(\s*([^,)]+)/g)) {
        const target = (call[1] ?? "").trim();
        assert.ok(/^"\/api\//.test(target), "src/" + relative + " may only fetch a same-origin app route, not " + target);
      }
    }
  }
  const surface = await readFile(new URL("../src/api/index.ts", import.meta.url), "utf8");
  assert.ok(surface.includes("./generated/index.ts"), "src/api/index.ts must present the generated client");
});
