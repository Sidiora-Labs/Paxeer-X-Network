import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir, open, readFile, stat } from "node:fs/promises";
import { isAbsolute, resolve } from "node:path";
import { StringDecoder } from "node:string_decoder";
import { referenceArtifacts } from "./support/artifacts.mjs";

const root = resolve(import.meta.dirname, "../..");
const environment = process.env.PAXEER_X_REFERENCE_APPS_ENVIRONMENT ?? "emulator";
assert.ok(["emulator", "beta"].includes(environment), "unknown reference environment");
const evidence = process.env.PAXEER_X_EVIDENCE_DIR;
assert.ok(typeof evidence === "string" && isAbsolute(evidence) && resolve(evidence) === evidence
  && !evidence.startsWith(`${root}/`), "private evidence directory required");
const info = await stat(evidence);
assert.ok(info.isDirectory() && info.uid === process.getuid() && (info.mode & 0o077) === 0, "unsafe evidence directory");
const artifacts = await referenceArtifacts(process.env.PAXEER_X_REFERENCE_APP_ARTIFACTS, root);
const directory = resolve(evidence, `reference-apps-${Date.now()}-${process.pid}`);
await mkdir(directory, { mode: 0o700 });
const log = await open(resolve(directory, "actual-processes.log"), "wx", 0o600);
const rows = [];
let pending = "";
let total = 0;
let code;
let processLog = Promise.resolve();
const decoder = new StringDecoder("utf8");
try {
  code = await new Promise((accept, reject) => {
    const child = spawn(process.execPath, ["platform/examples/run-reference-apps.mjs", "--scenario", environment], {
      cwd: root, env: { ...process.env, LAYERX_BIN: artifacts.cli.path,
        LAYERX_EXAMPLE_STATE_ROOT: resolve(directory, "state") }, stdio: ["ignore", "pipe", "pipe"], detached: true,
    });
    let failure;
    let killing;
    const terminate = () => {
      try { process.kill(-child.pid, "SIGTERM"); } catch (error) { if (error.code !== "ESRCH") failure ??= error; }
      killing ??= setTimeout(() => {
        try { process.kill(-child.pid, "SIGKILL"); } catch (error) { if (error.code !== "ESRCH") failure ??= error; }
      }, 10_000);
    };
    const fail = (error) => { failure ??= error; terminate(); };
    process.once("SIGTERM", terminate);
    process.once("SIGINT", terminate);
    const timeout = setTimeout(() => { fail(new Error("reference_runtime_deadline")); }, 20 * 60 * 1000);
    const write = async (chunk) => {
      total += chunk.length;
      if (total > 16 * 1024 * 1024) { fail(new Error("reference_output_bound")); return; }
      processLog = processLog.then(() => log.write(chunk));
      await processLog;
    };
    child.stderr.on("data", (chunk) => { void write(chunk).catch(fail); });
    child.stdout.on("data", (chunk) => {
      void write(chunk).catch(fail);
      pending += decoder.write(chunk);
      for (;;) {
        const newline = pending.indexOf("\n");
        if (newline < 0) break;
        const line = pending.slice(0, newline); pending = pending.slice(newline + 1);
        if (line.startsWith("{")) {
          try { rows.push(JSON.parse(line)); } catch { fail(new Error("invalid_reference_process_json")); }
        }
      }
    });
    child.once("error", fail);
    child.once("close", (result, signal) => {
      clearTimeout(timeout); clearTimeout(killing);
      process.removeListener("SIGTERM", terminate); process.removeListener("SIGINT", terminate);
      if (failure !== undefined) reject(failure);
      else if (signal !== null) reject(new Error("reference_process_interrupted"));
      else accept(result);
    });
  });
  assert.equal(code, 0, "actual reference application failed");
  const only = (predicate, name) => {
    const found = rows.filter(predicate);
    assert.equal(found.length, 1, `missing or duplicate real outcome: ${name}`);
    return found[0];
  };
  const seller = only((row) => row.application === "paid-api" && row.environment === environment && row.path === "/paid", "paid API process");
  assert.ok(Number.isSafeInteger(seller.listening) && seller.listening > 0);
  const buyer = only((row) => row.application === "buyer-agent" && row.environment === environment && row.state === "paid", "buyer payment");
  assert.equal(buyer.status, 200);
  assert.equal(buyer.verification, "sequencer-signed");
  assert.match(buyer.receiptDigest, /^[0-9a-f]{64}$/u);
  const declaredResource = JSON.parse(await readFile(resolve(root, "platform/examples/paid-api/resource.json"), "utf8"));
  assert.deepEqual(JSON.parse(buyer.body), declaredResource, "seller did not release actual declared resource after verified payment");
  const merchant = only((row) => row.application === "merchant-shop" && row.environment === environment && row.state === "paid", "verified merchant checkout");
  assert.equal(merchant.verification, "sequencer-signed");
  assert.match(merchant.receiptDigest, /^[0-9a-f]{64}$/u);
  assert.ok(typeof merchant.orderId === "string" && merchant.orderId.length > 0);
  const marketplace = [];
  for (const action of ["deploy", "list", "buy"]) {
    const outcome = only((row) => row.application === "marketplace" && row.environment === environment && row.action === action, `marketplace ${action}`);
    assert.equal(outcome.state, "completed");
    assert.equal(outcome.verification, "sequencer-signed");
    assert.equal(outcome.resultCode, 0);
    assert.match(outcome.receiptDigest, /^[0-9a-f]{64}$/u);
    marketplace.push(outcome);
  }
  assert.equal(new Set([buyer.receiptDigest, merchant.receiptDigest, ...marketplace.map((row) => row.receiptDigest)]).size, 5,
    "distinct real economic operations reused a receipt");
  const completed = only((row) => row.environment === environment && row.state === "completed" && Array.isArray(row.applications), "full reference suite");
  assert.deepEqual(completed.applications, ["buyer-agent", "paid-api", "merchant-shop", "marketplace"]);
  const record = { source_revision: artifacts.source_revision, environment, tests: 6, skipped: 0,
    buyer_receipt: buyer.receiptDigest, merchant_receipt: merchant.receiptDigest,
    marketplace: marketplace.map((row) => ({ action: row.action, receipt: row.receiptDigest, verification: row.verification })) };
  const output = await open(resolve(directory, "outcomes.json"), "wx", 0o600);
  try { await output.writeFile(`${JSON.stringify(record, null, 2)}\n`); await output.sync(); } finally { await output.close(); }
  process.stdout.write(`${JSON.stringify({ source_revision: artifacts.source_revision, environment, evidence: directory, tests: 6, skipped: 0 })}\n`);
  process.stdout.write("PAXEER_X_GATE tests=6 skipped=0\n");
} finally {
  await processLog;
  await log.sync();
  await log.close();
}
