import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { ed25519 } from "@noble/curves/ed25519.js";

import {
  PROGRAM_ABI_V5, ReceiptFailureCode, supportsProgramGuestAbi,
} from "../src/generated/receipt.js";
import {
  decodeNativeProgramCall, decodeNativeProgramCallV1, encodeNativeProgramCall, encodeNativeProgramCallV1,
  type NativeProgramCall, type NativeProgramCallV1,
} from "../src/native-program-call.js";
import {
  NativeProgramRequestV1, ProgramTrustContext, parseProgramExecutionDocumentV5, verifyProgramReceiptV5,
} from "../src/programs.js";
import {
  ReceiptVerificationError, programsModuleVersionForProtocol, verifyNativeProgramReceiptOutcomeV5, verifyReceipt,
  type AuthorizedReceiptBatch,
} from "../src/verifier.js";

const read = (path: string): Buffer => readFileSync(new URL(`../../../../../${path}`, import.meta.url));
const fixture = <T>(name: string): T => JSON.parse(read(`platform/sdk/conformance/fixtures/${name}.json`).toString("utf8")) as T;
const failsAt = (code: ReceiptFailureCode) => (error: unknown): boolean => error instanceof ReceiptVerificationError && error.check === code;

assert.equal(PROGRAM_ABI_V5, 5);
assert.deepEqual([0, 1, 2, 3, 4, 5, 6, 65_535].map(supportsProgramGuestAbi), [false, true, true, true, true, true, false, false]);
assert.deepEqual([3, 4, 5, 6].map(version => programsModuleVersionForProtocol(3, version, false)), [false, true, true, false]);
assert.deepEqual([1, 2, 3, 4, 5].map(version => programsModuleVersionForProtocol(2, version, false)), [true, true, true, false, false]);

const call = fixture<{ payload_hex: string; fee_limit: string; signed_activity_hex: string }>("native-program-call-v4");
const payloadV4 = Buffer.from(call.payload_hex, "hex");
assert.equal(payloadV4.readUInt16BE(32), 2);
const payloadV5 = Buffer.from(payloadV4);
payloadV5.writeUInt16BE(5, 32);
const decoded = decodeNativeProgramCallV1(payloadV5);
assert.equal(decoded.guestAbi, 5);
assert.deepEqual(Buffer.from(encodeNativeProgramCallV1(decoded)), payloadV5);
const fromV4 = decodeNativeProgramCallV1(payloadV4);
const callV5: NativeProgramCallV1 = { ...fromV4, guestAbi: 5 };
assert.deepEqual(Buffer.from(encodeNativeProgramCallV1(callV5)), payloadV5);
assert.deepEqual({ ...decoded, guestAbi: 2 }, fromV4);
assert.throws(() => decodeNativeProgramCall(payloadV5), TypeError);
assert.throws(() => encodeNativeProgramCall({ ...fromV4, guestAbi: 5 } as unknown as NativeProgramCall), TypeError);
for (const abi of [0, 6, 65_535]) {
  const refused = Buffer.from(payloadV4);
  refused.writeUInt16BE(abi, 32);
  assert.throws(() => decodeNativeProgramCallV1(refused), TypeError);
  assert.throws(() => encodeNativeProgramCallV1({ ...fromV4, guestAbi: abi } as unknown as NativeProgramCallV1), TypeError);
}
const request = new NativeProgramRequestV1(callV5, BigInt(call.fee_limit), Buffer.from(call.signed_activity_hex, "hex"));
assert.equal(request.nativeCall.guestAbi, 5);
assert.equal(request.programId, Buffer.from(callV5.programId).toString("hex"));
assert.equal(request.budget.fuel, callV5.resources[0]);

const executed = fixture<{
  canonical_receipt_hex: string; program_id_hex: string; terminal_payload_hex: string; call_graph_hex: string;
  authorized_batch: Record<"batch_id_hex" | "asset_hex" | "previous_state_root_hex" | "resulting_state_root_hex" | "sequencer_public_key_hex", string>;
}>("receipt-programs-executed-v4");
const original = Buffer.from(executed.canonical_receipt_hex, "hex");
const batch = executed.authorized_batch;
const authority: AuthorizedReceiptBatch = {
  batchId: Buffer.from(batch.batch_id_hex, "hex"), asset: Buffer.from(batch.asset_hex, "hex"),
  previousStateRoot: Buffer.from(batch.previous_state_root_hex, "hex"),
  resultingStateRoot: Buffer.from(batch.resulting_state_root_hex, "hex"),
  sequencerPublicKey: Buffer.from(batch.sequencer_public_key_hex, "hex"),
};
const sequencerSeed = new Uint8Array(32);
sequencerSeed[0] = 0x45;
assert.deepEqual(Buffer.from(ed25519.getPublicKey(sequencerSeed)), Buffer.from(authority.sequencerPublicKey));
const moduleHeader = Buffer.concat([Buffer.of(0, 0, 0, 32), authority.batchId, Buffer.of(0, 9)]);
const moduleVersionAt = original.indexOf(moduleHeader) + moduleHeader.length;
assert.equal(original.lastIndexOf(moduleHeader), moduleVersionAt - moduleHeader.length);
const outcomeAt = original.lastIndexOf(Buffer.from("PRG4", "latin1"));
assert(moduleVersionAt > moduleHeader.length && outcomeAt > moduleVersionAt);
assert.equal(original.readUInt32BE(moduleVersionAt), 4);
assert.deepEqual([original[outcomeAt + 4], original.readUInt16BE(outcomeAt + 9), original.readUInt16BE(outcomeAt + 11)], [1, 1, 2]);

function receiptWith(moduleVersion: number, abi: number): Buffer {
  const bytes = Buffer.from(original);
  bytes.writeUInt32BE(moduleVersion, moduleVersionAt);
  bytes.writeUInt16BE(abi, outcomeAt + 11);
  const digest = createHash("sha256").update("LXP/v1/receipt\0").update(bytes.subarray(0, -69)).update(Buffer.of(0)).digest();
  assert.deepEqual(bytes.subarray(-69, -64), Buffer.of(1, 0, 0, 0, 64));
  bytes.set(ed25519.sign(digest, sequencerSeed), bytes.length - 64);
  return bytes;
}

assert.deepEqual(receiptWith(4, 2), original);
const abi5 = receiptWith(5, 5);
const verified = await verifyNativeProgramReceiptOutcomeV5(abi5, authority);
const outcome = verified.receipt.programOutcome;
assert(outcome);
assert.deepEqual([verified.receipt.protocolVersion, verified.receipt.moduleId, verified.receipt.moduleVersion, verified.receipt.operation, verified.receipt.resultCode],
  [3, 9, 5, 3, 0]);
assert.deepEqual([outcome.abiVersion, outcome.runtimeVersion, outcome.encodingVersion, outcome.terminalKind, outcome.resultCode], [5, 1, 4, 1, 0]);
assert.deepEqual(Buffer.from(verified.receiptDigest),
  createHash("sha256").update("LXP/v1/receipt\0").update(abi5.subarray(0, -69)).update(Buffer.of(0)).digest());
assert.deepEqual(Buffer.from(verified.canonicalBytes), abi5);
for (const [moduleVersion, abi] of [[4, 3], [4, 4], [5, 3], [5, 4]] as const) {
  const accepted = await verifyNativeProgramReceiptOutcomeV5(receiptWith(moduleVersion, abi), authority);
  assert.deepEqual([accepted.receipt.moduleVersion, accepted.receipt.programOutcome?.abiVersion], [moduleVersion, abi]);
}
await assert.rejects(verifyNativeProgramReceiptOutcomeV5(receiptWith(4, 5), authority), failsAt(ReceiptFailureCode.ModuleVersion));
for (const moduleVersion of [3, 6]) {
  await assert.rejects(verifyNativeProgramReceiptOutcomeV5(receiptWith(moduleVersion, 5), authority), failsAt(ReceiptFailureCode.ModuleVersion));
}
for (const abi of [1, 2, 6]) {
  await assert.rejects(verifyNativeProgramReceiptOutcomeV5(receiptWith(5, abi), authority), failsAt(ReceiptFailureCode.ProtocolVersion));
}
const unsigned = Buffer.from(abi5);
unsigned.writeUInt16BE(4, outcomeAt + 11);
await assert.rejects(verifyNativeProgramReceiptOutcomeV5(unsigned, authority), failsAt(ReceiptFailureCode.SequencerSignature));
await assert.rejects(verifyNativeProgramReceiptOutcomeV5(abi5, { ...authority, sequencerPublicKey: ed25519.getPublicKey(new Uint8Array(32).fill(7)) }),
  failsAt(ReceiptFailureCode.SequencerSignature));
await assert.rejects(verifyReceipt(abi5, authority, { protocolVersion: 3 }), failsAt(ReceiptFailureCode.ProtocolVersion));
const legacyUnderV5 = await verifyReceipt(receiptWith(5, 2), authority, { protocolVersion: 3 });
assert.deepEqual([legacyUnderV5.receipt.moduleVersion, legacyUnderV5.receipt.programOutcome?.abiVersion], [5, 2]);
const documentFor = (moduleVersion: number, abi: number): Record<string, unknown> => {
  const bytes = receiptWith(moduleVersion, abi);
  return {
    state: "executed", activity_id: Buffer.from(verified.receipt.activityId).toString("hex"), program_id: executed.program_id_hex,
    guest_abi_version: abi, module_version: moduleVersion, batch_id: batch.batch_id_hex,
    global_sequence: verified.receipt.globalSequence.toString(), result_code: 0, state_root: batch.resulting_state_root_hex,
    receipt: bytes.toString("hex"),
    receipt_digest: createHash("sha256").update("LXP/v1/receipt\0").update(bytes.subarray(0, -69)).update(Buffer.of(0)).digest("hex"),
    terminal_payload: executed.terminal_payload_hex, call_graph: executed.call_graph_hex,
    authority: { batch_id: batch.batch_id_hex, asset: batch.asset_hex, previous_state_root: batch.previous_state_root_hex,
      resulting_state_root: batch.resulting_state_root_hex, sequencer_public_key: batch.sequencer_public_key_hex },
    usage: { cpu_fuel: outcome.cpuFuel.toString(), memory_bytes: outcome.memoryBytes.toString(),
      storage_read_bytes: outcome.storageReadBytes.toString(), storage_write_bytes: outcome.storageWriteBytes.toString(),
      output_values: outcome.outputValues, output_bytes: outcome.outputBytes.toString(), fee_units: outcome.feeUnits.toString() },
    outcome: { kind: "completed", code: 0, response: "" },
    verification: "receipt-terminal-and-call-graph-verified",
  };
};
const documentV5 = parseProgramExecutionDocumentV5(documentFor(5, 5));
assert.deepEqual([documentV5.guest_abi_version, documentV5.module_version, documentV5.receipt], [5, 5, abi5.toString("hex")]);
assert.equal(parseProgramExecutionDocumentV5(documentFor(4, 4)).guest_abi_version, 4);
for (const [moduleVersion, abi] of [[5, 6], [6, 5], [5, 2]] as const) {
  assert.throws(() => parseProgramExecutionDocumentV5(documentFor(moduleVersion, abi)), TypeError);
}
const trust = new ProgramTrustContext(authority.sequencerPublicKey, undefined, undefined, 3);
await assert.rejects(verifyProgramReceiptV5(parseProgramExecutionDocumentV5(documentFor(4, 5)), authority, trust),
  (error: unknown) => error instanceof TypeError && error.message === "invalid program execution evidence");
await assert.rejects(verifyProgramReceiptV5(documentV5, authority, new ProgramTrustContext(authority.sequencerPublicKey)),
  (error: unknown) => error instanceof TypeError && error.message === "invalid program execution evidence");
console.log("guest ABI 5 native call round-trips and its protocol 3 module 5 receipt outcome verifies");
