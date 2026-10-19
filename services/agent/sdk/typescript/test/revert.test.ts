import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { LXP_RETRYABLE, PlatformSdkError, RevertReasonError, decodeRevertReason } from "../src/index.js";

function encodeReason(reason: string): string {
  const bytes = new TextEncoder().encode(reason);
  const padded = new Uint8Array(Math.ceil(bytes.length / 32) * 32);
  padded.set(bytes);
  return `0x08c379a0${(32).toString(16).padStart(64, "0")}${bytes.length.toString(16).padStart(64, "0")}${Buffer.from(padded).toString("hex")}`;
}

function refused(returnData: string | Uint8Array): void {
  assert.throws(() => decodeRevertReason(returnData), (error: unknown) => {
    assert.ok(error instanceof PlatformSdkError);
    assert.equal(error.code, "decode-failure");
    assert.equal(error.retry, "never");
    return true;
  });
}

const table = readFileSync(new URL("../../../../../../spec/layerx-merge/contracts/address-table.md", import.meta.url), "utf8");
const section = table.slice(table.indexOf("## (b) Error code registry"), table.indexOf("## (c) Activation"));
const rows = section.split("\n").filter((line) => line.startsWith("| LXP_")).map((line) => {
  const cells = line.split("|").map((cell) => cell.trim());
  return { code: cells[1] ?? "", message: cells[2] ?? "", retryable: cells[cells.length - 2] };
});
assert.equal(rows.length, 274);
assert.equal(new Set(rows.map((row) => row.code)).size, rows.length);
for (const row of rows) assert.ok(row.retryable === "yes" || row.retryable === "no", row.code);
assert.deepEqual([...LXP_RETRYABLE].sort(), rows.filter((row) => row.retryable === "yes").map((row) => row.code).sort());
assert.equal(LXP_RETRYABLE.size, 37);

for (const row of rows) {
  const decoded = decodeRevertReason(encodeReason(`${row.code}: ${row.message}`));
  assert.ok(decoded instanceof RevertReasonError);
  assert.ok(decoded instanceof Error);
  assert.equal(decoded.name, "RevertReasonError");
  assert.equal(decoded.code, row.code);
  assert.equal(decoded.message, row.message);
  assert.equal(decoded.detail, undefined);
  assert.equal(decoded.retryable, row.retryable === "yes", row.code);
  assert.equal(decoded.retry, row.retryable === "yes" ? "safe" : "never", row.code);
}

const truncated = decodeRevertReason(encodeReason("LXP_TRUNCATED: truncated"));
assert.deepEqual([truncated.code, truncated.message, truncated.retryable, truncated.retry], ["LXP_TRUNCATED", "truncated", false, "never"]);
const disabled = decodeRevertReason(encodeReason("LXP_MODULE_DISABLED: module disabled"));
assert.deepEqual([disabled.code, disabled.retryable, disabled.retry], ["LXP_MODULE_DISABLED", false, "never"]);
for (const code of ["LXP_PROJECTION_STALE", "LXP_IO", "LXP_PAUSED_SCOPE", "LXP_XWEB_PAUSED"]) {
  assert.equal(decodeRevertReason(encodeReason(`${code}: x`)).retryable, true, code);
}

const detailVector = "0x08c379a0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000354c58505f554e415554484f52495a45445f44454249543a20756e617574686f72697a65642064656269743b2066726f6d3d307830310000000000000000000000";
assert.equal(encodeReason("LXP_UNAUTHORIZED_DEBIT: unauthorized debit; from=0x01"), detailVector);
const withDetail = decodeRevertReason(detailVector);
assert.deepEqual(
  [withDetail.code, withDetail.message, withDetail.detail, withDetail.retryable, withDetail.retry],
  ["LXP_UNAUTHORIZED_DEBIT", "unauthorized debit", "from=0x01", false, "never"],
);
const fromBytes = decodeRevertReason(Uint8Array.from(Buffer.from(detailVector.slice(2), "hex")));
assert.deepEqual([fromBytes.code, fromBytes.message, fromBytes.detail], ["LXP_UNAUTHORIZED_DEBIT", "unauthorized debit", "from=0x01"]);
const colonMessage = decodeRevertReason(encodeReason("LXP_FEETOKEN_SWITCH_OFF: fee token preference: fee-token switch is off; token=SID"));
assert.deepEqual(
  [colonMessage.code, colonMessage.message, colonMessage.detail, colonMessage.retryable],
  ["LXP_FEETOKEN_SWITCH_OFF", "fee token preference: fee-token switch is off", "token=SID", true],
);
assert.equal(decodeRevertReason(encodeReason("LXP_ADDR_EVM_NOT_ASSOCIATED: EVM address is not associated")).retryable, true);
assert.equal(decodeRevertReason(encodeReason(`LXP_TRUNCATED: ${"a".repeat(64)}`)).message, "a".repeat(64));

for (const code of ["LXP_NOT_IN_REGISTRY", "LXP_UNKNOWN", "LXP_FATAL_INVARIANT", "LXP_X1"]) {
  const unknown = decodeRevertReason(encodeReason(`${code}: something new`));
  assert.deepEqual([unknown.code, unknown.message, unknown.detail, unknown.retryable, unknown.retry], [code, "something new", undefined, false, "never"]);
}

const valid = encodeReason("LXP_IO: io");
refused("");
refused("0x");
refused(valid.slice(2));
refused(`${valid}0`);
refused(`${valid.slice(0, -2)}zz`);
refused(new Uint8Array());
refused(`0x4e487b71${"11".padStart(64, "0")}`);
refused(`0x12345678${valid.slice(10)}`);
refused(`0x08c379a0${(64).toString(16).padStart(64, "0")}${valid.slice(74)}`);
refused(`0x08c379a0${"01".padEnd(64, "0")}${valid.slice(74)}`);
refused(valid.slice(0, -64));
refused(`${valid}${"00".repeat(32)}`);
refused(`${valid.slice(0, -2)}01`);
refused(`0x08c379a0${(32).toString(16).padStart(64, "0")}${"ff".repeat(32)}`);
refused(encodeReason(""));
refused(encodeReason("insufficient funds"));
refused(encodeReason("execution reverted"));
refused(encodeReason("LXP_IO"));
refused(encodeReason("LXP_IO:io"));
refused(encodeReason("LXP_IO: "));
refused(encodeReason("LXP_IO: io; "));
refused(encodeReason("lxp_io: io"));
refused(encodeReason("LXP_: io"));
refused(encodeReason("LXP_IO_: io"));
refused(encodeReason("LXP__IO: io"));
refused(encodeReason("XLXP_IO: io"));
refused(encodeReason(" LXP_IO: io"));
refused(`0x08c379a0${(32).toString(16).padStart(64, "0")}${(2).toString(16).padStart(64, "0")}${"c328".padEnd(64, "0")}`);
refused(`0x08c379a0${(32).toString(16).padStart(64, "0")}${(65 * 1024).toString(16).padStart(64, "0")}${"41".repeat(65 * 1024)}`);
