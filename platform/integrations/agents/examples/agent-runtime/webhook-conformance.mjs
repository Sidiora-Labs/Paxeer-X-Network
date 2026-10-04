import assert from "node:assert/strict";
import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  AGENT_FRAMEWORKS,
  MAXIMUM_WEBHOOK_BYTES,
  WEBHOOK_ID_HEADER,
  WEBHOOK_KEY_HEADER,
  WEBHOOK_SIGNATURE_HEADER,
  WEBHOOK_TIMESTAMP_HEADER,
  loadAgentServiceProviders,
} from "@sidiora/layerx-agent-integrations";

const required = (name) => {
  const value = process.env[name];
  if (value === undefined || value.length === 0) throw new Error(`missing_${name.toLowerCase()}`);
  return value;
};

const framework = required("LAYERX_AGENT_FRAMEWORK");
if (!AGENT_FRAMEWORKS.includes(framework)) throw new Error("unsupported_framework");
const expected = required("LAYERX_WEBHOOK_EXPECT");
if (expected !== "processed" && expected !== "duplicate") throw new Error("invalid_webhook_expectation");
required("LAYERX_WEBHOOK_DELIVERY_STORE_PATH");

if (process.argv[2] === "--hold-coordination-lock") {
  const { DatabaseSync } = await import("node:sqlite");
  const ledger = resolve(required("LAYERX_WEBHOOK_DELIVERY_STORE_PATH"));
  const database = new DatabaseSync(`${ledger}.coordination.sqlite3`, { timeout: 0 });
  try {
    assert.equal(database.prepare("PRAGMA application_id").get().application_id, 0x4c585748);
    assert.equal(database.prepare("SELECT ledger_path FROM delivery_context WHERE singleton = 1").get().ledger_path, ledger);
    database.exec("BEGIN IMMEDIATE");
    process.stdout.write(JSON.stringify({ coordination: "held" }) + "\n");
    await new Promise((finish) => setTimeout(finish, 30_000));
    throw new Error("coordination_crash_not_exercised");
  } finally {
    if (database.isTransaction) database.exec("ROLLBACK");
    database.close();
  }
}

async function crashCoordinationOwner() {
  const child = spawn(process.execPath, [fileURLToPath(import.meta.url), "--hold-coordination-lock"], {
    stdio: ["ignore", "pipe", "ignore"],
  });
  const exited = once(child, "exit").then((value) => ({ value }), () => ({ error: true }));
  try {
    await new Promise((ready, reject) => {
      let text = "";
      const timeout = setTimeout(() => reject(new Error("coordination_owner_timeout")), 10_000);
      const cleanup = () => { clearTimeout(timeout); child.stdout.removeListener("data", read); };
      const read = (chunk) => {
        text += chunk.toString("utf8");
        if (text.length > 1_024) { cleanup(); reject(new Error("invalid_coordination_owner")); return; }
        const newline = text.indexOf("\n");
        if (newline !== -1) {
          cleanup();
          try { assert.deepEqual(JSON.parse(text.slice(0, newline)), { coordination: "held" }); ready(); }
          catch { reject(new Error("invalid_coordination_owner")); }
        }
      };
      child.stdout.on("data", read);
      child.once("error", () => { cleanup(); reject(new Error("coordination_owner_failed")); });
      child.once("exit", () => { cleanup(); reject(new Error("coordination_owner_exited")); });
    });
    assert.equal(child.kill("SIGKILL"), true);
    const exit = await exited;
    assert.deepEqual(exit, { value: [null, "SIGKILL"] });
    return { signal: "SIGKILL", recovered: true };
  } finally {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
    await exited;
  }
}

const input = await open(required("LAYERX_WEBHOOK_DELIVERY_PATH"), constants.O_RDONLY | constants.O_NOFOLLOW);
let capture;
try {
  const metadata = await input.stat();
  if (!metadata.isFile() || metadata.size > MAXIMUM_WEBHOOK_BYTES * 2) throw new Error("invalid_webhook_capture");
  capture = JSON.parse(await input.readFile("utf8"));
} finally {
  await input.close();
}
if (capture === null || typeof capture !== "object" || Array.isArray(capture)
    || typeof capture.body !== "string" || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/u.test(capture.body)
    || capture.headers === null || typeof capture.headers !== "object" || Array.isArray(capture.headers)) {
  throw new Error("invalid_webhook_capture");
}
const body = new Uint8Array(Buffer.from(capture.body, "base64"));
if (body.length === 0 || body.length > MAXIMUM_WEBHOOK_BYTES) throw new Error("invalid_webhook_capture");
const headers = Object.create(null);
for (const [name, value] of Object.entries(capture.headers)) {
  if (typeof value !== "string" || Object.hasOwn(headers, name.toLowerCase())) throw new Error("invalid_webhook_capture");
  headers[name.toLowerCase()] = value;
}
for (const name of [WEBHOOK_ID_HEADER, WEBHOOK_TIMESTAMP_HEADER, WEBHOOK_KEY_HEADER, WEBHOOK_SIGNATURE_HEADER]) {
  if (!Object.hasOwn(headers, name)) throw new Error("invalid_webhook_capture");
}

const providers = await loadAgentServiceProviders(process.env);
let integration;
let taskStore;
try {
  const options = { environment: process.env, ...providers };
  if (framework === "a2a") {
    const { createA2AIntegration, FileA2ATaskStore } = await import("@sidiora/layerx-agent-integrations/a2a");
    taskStore = new FileA2ATaskStore(`${required("LAYERX_WEBHOOK_DELIVERY_STORE_PATH")}.a2a-tasks.sqlite3`);
    const tenant = required("LAYERX_TENANT");
    const actor = required("LAYERX_ACTOR");
    integration = createA2AIntegration({
      ...options,
      durableTaskStore: taskStore,
      authentication: {
        schemeName: "layerxBearer",
        securityScheme: { scheme: { $case: "httpAuthSecurityScheme", value: {
          description: "LayerX service bearer token", scheme: "Bearer", bearerFormat: "opaque",
        } } },
        async authenticate(context) {
          if (context.tenant !== tenant || context.user?.isAuthenticated !== true || context.user.userName !== actor) {
            throw new Error("a2a_unauthenticated_request_context");
          }
          return context;
        },
      },
    });
  } else {
    const factory = {
      mcp: ["mcp", "createMcpIntegration"],
      openai: ["openai", "createOpenAiIntegration"],
      anthropic: ["anthropic", "createAnthropicIntegration"],
      langchain: ["langchain", "createLangChainIntegration"],
      "vercel-ai": ["vercel-ai", "createVercelAiIntegration"],
    }[framework];
    const module = await import(`@sidiora/layerx-agent-integrations/${factory[0]}`);
    integration = module[factory[1]](options);
  }
  let handled = 0;
  const handler = { async handle(_event, deliveryId) {
    assert.equal(deliveryId, headers[WEBHOOK_ID_HEADER]);
    handled += 1;
  } };
  const gateway = integration.webhooks;
  const tampered = body.slice();
  tampered[tampered.length - 1] ^= 1;
  const tamper = await gateway.respond(tampered, headers, handler);
  assert.equal(tamper.status, 401);
  assert.deepEqual(JSON.parse(tamper.body), { error: "invalid-webhook" });
  const missingHeaders = { ...headers };
  delete missingHeaders[WEBHOOK_SIGNATURE_HEADER];
  const missing = await gateway.respond(body, missingHeaders, handler);
  assert.equal(missing.status, 401);
  const duplicate = await gateway.respond(body, {
    ...headers, [WEBHOOK_SIGNATURE_HEADER.toUpperCase()]: headers[WEBHOOK_SIGNATURE_HEADER],
  }, handler);
  assert.equal(duplicate.status, 400);
  assert.deepEqual(JSON.parse(duplicate.body), { error: "duplicate-header" });
  const stale = await gateway.respond(body, { ...headers, [WEBHOOK_TIMESTAMP_HEADER]: "0" }, handler);
  assert.equal(stale.status, 401);
  assert.equal(handled, 0);

  let handlerFailures = 0;
  if (expected === "processed") {
    const interruption = new Error("webhook_handler_interrupted");
    await assert.rejects(gateway.consume(body, headers, { async handle() {
      handlerFailures += 1;
      throw interruption;
    } }), (error) => error === interruption);
    assert.equal(handlerFailures, 1);
  }

  const mixedCaseHeaders = Object.fromEntries(Object.entries(headers).map(([name, value]) => [
    name.split("-").map((word) => word[0].toUpperCase() + word.slice(1)).join("-"), value,
  ]));
  const first = await gateway.respond(body, mixedCaseHeaders, handler);
  assert.equal(first.status, 200);
  assert.deepEqual(JSON.parse(first.body), { outcome: expected });
  const lockRecovery = await crashCoordinationOwner();
  const second = await gateway.respond(body, headers, handler);
  assert.equal(second.status, 200);
  assert.deepEqual(JSON.parse(second.body), { outcome: "duplicate" });
  assert.equal(handled, expected === "processed" ? 1 : 0);
  process.stdout.write(JSON.stringify({
    framework, expected, handled, handlerFailures, lockRecovery,
    tamper: { status: tamper.status, ...JSON.parse(tamper.body) },
    missing: { status: missing.status, ...JSON.parse(missing.body) },
    duplicateHeaders: { status: duplicate.status, ...JSON.parse(duplicate.body) },
    stale: { status: stale.status, ...JSON.parse(stale.body) },
    first: { status: first.status, ...JSON.parse(first.body) },
    second: { status: second.status, ...JSON.parse(second.body) },
  }) + "\n");
} finally {
  try {
    if (framework === "mcp") await integration?.closeMcp();
  } finally {
    try { integration?.destroy(); }
    finally {
      try { taskStore?.close(); }
      finally { await providers.destroy?.(); }
    }
  }
}
