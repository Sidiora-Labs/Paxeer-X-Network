import assert from "node:assert/strict";
import { lstatSync, readFileSync } from "node:fs";
import { ProgramTrustContext, parseProgramExecutionDocumentV5, parseProgramExecutionDocument,
  verifyProgramReceiptV5, verifyProgramReceipt } from "../../../agent/sdk/typescript/dist/src/programs.js";
import { supportedProgramGuestAbi } from "../../../agent/sdk/typescript/dist/src/verifier.js";

const path = process.env.PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS;
if (!path) {
  console.error("genuine native ABI3/4 terminal-v5 corpus required");
  process.exit(78);
}
const info = lstatSync(path);
assert(info.isFile() && !info.isSymbolicLink() && info.size > 0 && info.size <= 16_777_216);
const corpus = JSON.parse(readFileSync(path, "utf8"));
assert.equal(corpus.source_revision, process.env.PAXEER_X_MAINLINE);
assert.match(corpus.trusted_sequencer_public_key_hex, /^[0-9a-f]{64}$/);
assert(Array.isArray(corpus.cases));
const expected = new Set([3, 4].flatMap(abi => ["success", "failure", "resource", "callback", "settlement"].map(kind => `${abi}:${kind}`)));
const present = new Set();
let verifiedCases = 0;
const hex = (value, bytes) => {
  assert(typeof value === "string" && /^(?:[0-9a-f]{2})+$/.test(value));
  const result = Buffer.from(value, "hex");
  if (bytes !== undefined) assert.equal(result.length, bytes);
  return result;
};
const flip = value => {
  const result = hex(value);
  result[result.length - 1] ^= 1;
  return result.toString("hex");
};
for (const row of corpus.cases) {
  const selector = `${row.guest_abi}:${row.outcome}`;
  assert(expected.has(selector) && !present.has(selector), `missing, duplicate or unknown genuine case: ${selector}`);
  present.add(selector);
  assert.equal(row.sequencer_public_key_hex, corpus.trusted_sequencer_public_key_hex);
  const document = parseProgramExecutionDocumentV5(row.execution);
  assert.equal(document.guest_abi_version, row.guest_abi);
  assert.equal(document.program_id, row.program_id_hex);
  assert.equal(document.receipt, row.canonical_receipt_hex);
  assert.equal(document.terminal_payload, row.terminal_payload_hex);
  assert.equal(document.call_graph, row.call_graph_hex);
  assert.equal(document.authority.sequencer_public_key, corpus.trusted_sequencer_public_key_hex);
  const authority = {
    batchId: hex(document.authority.batch_id, 32), asset: hex(document.authority.asset, 32),
    previousStateRoot: hex(document.authority.previous_state_root, 32),
    resultingStateRoot: hex(document.authority.resulting_state_root, 32),
    sequencerPublicKey: hex(corpus.trusted_sequencer_public_key_hex, 32),
  };
  const trust = new ProgramTrustContext(authority.sequencerPublicKey, () => BigInt(Date.now()), 300_000n, 3);
  const signed = hex(row.signed_activity_hex);
  const check = (value, batch = authority, activity = signed) => verifyProgramReceiptV5(value, batch, trust, activity);
  const verified = await check(document);
  assert.equal(verified.verification.level, "sequencer-signed");
  assert.equal(verified.verification.receipt.programOutcome.abiVersion, row.guest_abi);
  assert.equal(document.outcome.kind, row.outcome === "success" ? "completed" : "refused");
  assert.equal(document.state, row.outcome === "success" ? "executed" : "refused");
  assert.equal(supportedProgramGuestAbi(row.guest_abi), false);
  assert.throws(() => parseProgramExecutionDocument(row.execution));
  await assert.rejects(verifyProgramReceipt(document, authority, trust, signed));
  for (const abi of [0, 1, 2, 5, 65535, row.guest_abi === 3 ? 4 : 3]) {
    await assert.rejects(check({ ...document, guest_abi_version: abi }));
  }
  for (const field of ["receipt", "terminal_payload", "call_graph", "activity_id", "receipt_digest", "state_root"]) {
    await assert.rejects(check({ ...document, [field]: flip(document[field]) }));
  }
  for (const field of ["batchId", "previousStateRoot", "resultingStateRoot", "sequencerPublicKey"]) {
    const altered = Buffer.from(authority[field]); altered[0] ^= 1;
    await assert.rejects(check(document, { ...authority, [field]: altered }));
  }
  await assert.rejects(check({ ...document, usage: { ...document.usage, cpu_fuel: (BigInt(document.usage.cpu_fuel) + 1n).toString() } }));
  await assert.rejects(check({ ...document, module_version: 3 }));
  await assert.rejects(check({ ...document, global_sequence: (BigInt(document.global_sequence) + 1n).toString() }));
  const { retained_signed_activity: retained, ...withoutRetained } = document;
  await assert.rejects(verifyProgramReceiptV5(withoutRetained, authority, trust));
  const wrongRequest = Buffer.from(signed); wrongRequest[wrongRequest.length - 1] ^= 1;
  await assert.rejects(check(document, authority, wrongRequest));
  for (const length of [0, 1, 105, hex(document.terminal_payload).length - 1]) {
    const truncated = hex(document.terminal_payload).subarray(0, Math.max(0, length));
    await assert.rejects(check({ ...document, terminal_payload: truncated.toString("hex") }));
  }
  await assert.rejects(check({ ...document, terminal_payload: document.terminal_payload + "00" }));
  console.log(`TYPESCRIPT_PROGRAM_V5_CASE ${selector}`);
  verifiedCases += 1;
}
assert.deepEqual(present, expected);
assert.equal(verifiedCases, 10);
console.log("TypeScript verified 10 genuine native ABI3/4 outcomes; legacy and adversarial refusals retained");
