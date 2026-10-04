import { MAX_STORAGE_KEY_BYTES, MAX_STORAGE_VALUE_BYTES, MAX_CALL_INPUT_BYTES, MAX_CAPABILITY_ENCODING_BYTES,
  MAX_CALL_RESPONSE_BYTES, MAX_PROGRAM_ACCOUNT_SEED_BYTES, MAX_REFUSAL_REASON_BYTES } from "./abi";
import { copy, fromString, pointer, slice, isZero, compare, readU16BE, readU32BE, readU64BE } from "./bytes";
import { Amount } from "./amount";
import { AccountId, AssetId, ProgramId } from "./ids";
import { ERR_BOUNDS, ERR_INVALID, ERR_BUFFER_TOO_SMALL, ERR_RESERVED_IDENTIFIER, ERR_ZERO_AMOUNT, OK } from "./error";

@external("layerx_v2", "response_write")
declare function v2ResponseWrite(p0: i32, p1: i32, p2: i32): i32;

@external("layerx_v2", "program_call_response")
declare function v2ProgramCallResponse(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32, p7: i32): i64;

@external("layerx_v2", "refusal_write")
declare function v2RefusalWrite(p0: i32, p1: i32, p2: i32): i32;

@external("layerx_v2", "storage_read_scoped")
declare function v2StorageReadScoped(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32): i32;

@external("layerx_v2", "storage_write_scoped")
declare function v2StorageWriteScoped(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32): i32;

@external("layerx_v2", "storage_delete_scoped")
declare function v2StorageDeleteScoped(p0: i32, p1: i32, p2: i32): i32;

@external("layerx_v2", "storage_drop_scoped")
declare function v2StorageDropScoped(p0: i32): i32;

@external("layerx_v2", "storage_scan_scoped")
declare function v2StorageScanScoped(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32, p7: i32, p8: i32): i32;

@external("layerx_v2", "transfer_program_402")
declare function v2TransferProgram402(p0: i64, p1: i64, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32, p7: i32, p8: i32, p9: i32): i32;

@external("layerx_v2", "fund_program_402")
declare function v2FundProgram402(p0: i64, p1: i64, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32, p7: i32): i32;

@external("layerx_v2", "context_read")
declare function v2ContextRead(p0: i32, p1: i32, p2: i32): i32;

@external("layerx_v2", "balance_read")
declare function v2BalanceRead(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32): i32;

@external("layerx_v2", "hash")
declare function v2Hash(p0: i32, p1: i32, p2: i32, p3: i32): i32;

@external("layerx_v2", "signature_verify")
declare function v2SignatureVerify(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32): i32;

@external("layerx_v2", "signature_recover")
declare function v2SignatureRecover(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32): i32;

@external("layerx_v2", "bigint_mul_256")
declare function v2BigintMul256(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32): i32;

@external("layerx_v2", "bigint_div_256")
declare function v2BigintDiv256(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32): i32;

@external("layerx_v2", "bigint_rem_256")
declare function v2BigintRem256(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32): i32;

@external("layerx_v2", "bigint_modexp_256")
declare function v2BigintModexp256(p0: i32, p1: i32, p2: i32, p3: i32, p4: i32, p5: i32, p6: i32, p7: i32): i32;

@external("layerx_v3", "oracle_read")
declare function v3OracleRead(p0: i32, p1: i32, p2: i32, p3: i32): i32;

@external("layerx_v4", "web_read")
declare function v4WebRead(p0: i32, p1: i32, p2: i32, p3: i32): i32;

export const STATUS_VERIFY_FAILED: i32 = -6;
export const STATUS_WEB_ABSENT: i32 = -7;
export const STORAGE_PRINCIPAL: i32 = 1;
export const STORAGE_SHARED: i32 = 2;
export const MAX_SCAN_CURSOR_BYTES: i32 = 591;
export const MAX_SCAN_ENTRIES: i32 = 64;
export const MAX_SCAN_BYTES: i32 = 67126228;
export const HASH_SHA256: i32 = 1;
export const HASH_KECCAK256: i32 = 2;
export const HASH_BLAKE3: i32 = 3;
export const CONTEXT_EXECUTING_PROGRAM: i32 = 1;
export const CONTEXT_IMMEDIATE_CALLER: i32 = 2;
export const CONTEXT_INVOKING_PRINCIPAL: i32 = 3;
export const CONTEXT_ACTIVITY_SEQUENCE: i32 = 4;
export const CONTEXT_BATCH_HEIGHT: i32 = 5;
export const CONTEXT_RUNTIME_VERSION: i32 = 6;
export const CONTEXT_ABI_VERSION: i32 = 7;
export const CONTEXT_REMAINING_FUEL: i32 = 8;
export const CONTEXT_FEE_SCHEDULE_VERSION: i32 = 9;

export class VersionedBytes {
  readonly status: i32;
  readonly found: bool;
  readonly bytes: StaticArray<u8>;
  constructor(status: i32, found: bool, bytes: StaticArray<u8>) {
    this.status = status; this.found = found; this.bytes = bytes;
  }
  ok(): bool { return this.status == OK; }
}
function failed(status: i32): VersionedBytes {
  return new VersionedBytes(status, false, new StaticArray<u8>(0));
}
function exactVersioned(status: i32, expected: i32): i32 {
  return status < 0 ? status : status == expected ? OK : ERR_INVALID;
}
function initialized(status: i32, output: StaticArray<u8>, expected: i32): VersionedBytes {
  const checked = exactVersioned(status, expected);
  return checked == OK ? new VersionedBytes(OK, true, slice(output, 0, expected)) : failed(checked);
}
function validSelector(selector: i32): bool { return selector == STORAGE_PRINCIPAL || selector == STORAGE_SHARED; }
function validKey(key: StaticArray<u8>): bool { return key.length > 0 && key.length <= MAX_STORAGE_KEY_BYTES; }

export function publishResponseV2(code: i32, bytes: StaticArray<u8>): i32 {
  if (code < 0) return ERR_INVALID;
  if (bytes.length > MAX_CALL_RESPONSE_BYTES) return ERR_BOUNDS;
  return exactVersioned(v2ResponseWrite(code, bytes.length == 0 ? 0 : pointer(bytes), bytes.length), 0);
}
export function publishRefusalV2(refusalClass: i32, reason: StaticArray<u8>): i32 {
  if (refusalClass < 1 || refusalClass > 5) return ERR_INVALID;
  if (reason.length > MAX_REFUSAL_REASON_BYTES) return ERR_BOUNDS;
  return exactVersioned(v2RefusalWrite(refusalClass, reason.length == 0 ? 0 : pointer(reason), reason.length), 0);
}
export class CallResponseV2 {
  readonly status: i32;
  readonly code: i32;
  readonly bytes: StaticArray<u8>;
  constructor(status: i32, code: i32, bytes: StaticArray<u8>) { this.status = status; this.code = code; this.bytes = bytes; }
  ok(): bool { return this.status == OK; }
}
export function decodeCallResponseV2(packed: i64, output: StaticArray<u8>): CallResponseV2 {
  if (packed < 0) {
    const status = packed < <i64>i32.MIN_VALUE ? ERR_INVALID : <i32>packed;
    return new CallResponseV2(status, 0, new StaticArray<u8>(0));
  }
  const length = <u32><u64>packed;
  if (<u64>length > <u64>output.length || output.length > MAX_CALL_RESPONSE_BYTES) {
    return new CallResponseV2(ERR_BUFFER_TOO_SMALL, 0, new StaticArray<u8>(0));
  }
  return new CallResponseV2(OK, <i32>(<u64>packed >> 32), slice(output, 0, <i32>length));
}
export function callProgramResponseV2(callee: ProgramId, input: StaticArray<u8>, capabilities: StaticArray<u8>, output: StaticArray<u8>): CallResponseV2 {
  if (callee.isReserved()) return new CallResponseV2(ERR_RESERVED_IDENTIFIER, 0, new StaticArray<u8>(0));
  if (input.length > MAX_CALL_INPUT_BYTES || capabilities.length > MAX_CAPABILITY_ENCODING_BYTES || output.length > MAX_CALL_RESPONSE_BYTES) {
    return new CallResponseV2(ERR_BOUNDS, 0, new StaticArray<u8>(0));
  }
  if (capabilities.length < 2) return new CallResponseV2(ERR_INVALID, 0, new StaticArray<u8>(0));
  const program = callee.bytes;
  return decodeCallResponseV2(v2ProgramCallResponse(pointer(program), 32, pointer(input), input.length,
    pointer(capabilities), capabilities.length, output.length == 0 ? 0 : pointer(output), output.length), output);
}
export function readScopedV2(selector: i32, key: StaticArray<u8>, output: StaticArray<u8>): VersionedBytes {
  if (!validSelector(selector)) return failed(ERR_INVALID);
  if (!validKey(key) || output.length > MAX_STORAGE_VALUE_BYTES) return failed(ERR_BOUNDS);
  const status = v2StorageReadScoped(selector, pointer(key), key.length, pointer(output), output.length);
  if (status < 0) return failed(status);
  if (status == 0) return new VersionedBytes(OK, false, new StaticArray<u8>(0));
  if (status - 1 > output.length) return failed(ERR_INVALID);
  return new VersionedBytes(OK, true, slice(output, 0, status - 1));
}
export function writeScopedV2(selector: i32, key: StaticArray<u8>, value: StaticArray<u8>): i32 {
  if (!validSelector(selector)) return ERR_INVALID;
  if (!validKey(key) || value.length > MAX_STORAGE_VALUE_BYTES) return ERR_BOUNDS;
  return exactVersioned(v2StorageWriteScoped(selector, pointer(key), key.length, pointer(value), value.length), 0);
}
export function deleteScopedV2(selector: i32, key: StaticArray<u8>): i32 {
  if (!validSelector(selector)) return ERR_INVALID;
  if (!validKey(key)) return ERR_BOUNDS;
  return exactVersioned(v2StorageDeleteScoped(selector, pointer(key), key.length), 0);
}
export function dropScopedV2(selector: i32): i32 {
  if (!validSelector(selector)) return ERR_INVALID;
  return exactVersioned(v2StorageDropScoped(selector), 0);
}
export class ScanEntryV2 {
  readonly key: StaticArray<u8>;
  readonly value: StaticArray<u8>;
  constructor(key: StaticArray<u8>, value: StaticArray<u8>) { this.key = key; this.value = value; }
}
export class ScanPageV2 {
  readonly status: i32;
  readonly entries: Array<ScanEntryV2>;
  readonly cursor: StaticArray<u8>;
  constructor(status: i32, entries: Array<ScanEntryV2>, cursor: StaticArray<u8>) {
    this.status = status; this.entries = entries; this.cursor = cursor;
  }
  ok(): bool { return this.status == OK; }
}
function failedScan(status: i32): ScanPageV2 { return new ScanPageV2(status, new Array<ScanEntryV2>(), new StaticArray<u8>(0)); }
export function decodeScanPageV2(encoded: StaticArray<u8>, prefix: StaticArray<u8>, maxEntries: i32, maxBytes: i32): ScanPageV2 {
  if (maxEntries < 1 || maxEntries > MAX_SCAN_ENTRIES || maxBytes < 5 || maxBytes > MAX_SCAN_BYTES || prefix.length > MAX_STORAGE_KEY_BYTES) return failedScan(ERR_BOUNDS);
  if (encoded.length < 5 || encoded.length > maxBytes) return failedScan(ERR_INVALID);
  const count = <i32>readU16BE(encoded, 0);
  if (count > maxEntries) return failedScan(ERR_INVALID);
  let offset = 2;
  const entries = new Array<ScanEntryV2>();
  for (let i = 0; i < count; i++) {
    if (offset > encoded.length - 2) return failedScan(ERR_INVALID);
    const keyLength = <i32>readU16BE(encoded, offset); offset += 2;
    if (keyLength == 0 || keyLength > MAX_STORAGE_KEY_BYTES || keyLength > encoded.length - offset) return failedScan(ERR_INVALID);
    const key = slice(encoded, offset, keyLength); offset += keyLength;
    if (keyLength < prefix.length || compare(key, 0, prefix, 0, prefix.length) != 0) return failedScan(ERR_INVALID);
    if (entries.length > 0) {
      const prior = entries[entries.length - 1].key;
      const common = prior.length < key.length ? prior.length : key.length;
      const order = compare(prior, 0, key, 0, common);
      if (order > 0 || (order == 0 && prior.length >= key.length)) return failedScan(ERR_INVALID);
    }
    if (offset > encoded.length - 4) return failedScan(ERR_INVALID);
    const valueLength = readU32BE(encoded, offset); offset += 4;
    if (valueLength > <u32>MAX_STORAGE_VALUE_BYTES || valueLength > <u32>(encoded.length - offset)) return failedScan(ERR_INVALID);
    const value = slice(encoded, offset, <i32>valueLength); offset += <i32>valueLength;
    entries.push(new ScanEntryV2(key, value));
  }
  if (offset > encoded.length - 3) return failedScan(ERR_INVALID);
  const present = encoded[offset++];
  const cursorLength = <i32>readU16BE(encoded, offset); offset += 2;
  if (present > 1 || cursorLength > MAX_SCAN_CURSOR_BYTES || cursorLength != encoded.length - offset ||
    (present == 0 && cursorLength != 0) || (present == 1 && cursorLength == 0)) return failedScan(ERR_INVALID);
  return new ScanPageV2(OK, entries, slice(encoded, offset, cursorLength));
}
export function scanScopedV2(selector: i32, prefix: StaticArray<u8>, cursor: StaticArray<u8>, maxEntries: i32, maxBytes: i32, output: StaticArray<u8>): ScanPageV2 {
  if (!validSelector(selector)) return failedScan(ERR_INVALID);
  if (prefix.length > MAX_STORAGE_KEY_BYTES || cursor.length > MAX_SCAN_CURSOR_BYTES || maxEntries < 1 || maxEntries > MAX_SCAN_ENTRIES || maxBytes < 5 || maxBytes > MAX_SCAN_BYTES) return failedScan(ERR_BOUNDS);
  const status = v2StorageScanScoped(selector, pointer(prefix), prefix.length, pointer(cursor), cursor.length, maxEntries, maxBytes, pointer(output), output.length);
  if (status < 0) return failedScan(status);
  if (status > output.length) return failedScan(ERR_INVALID);
  return decodeScanPageV2(slice(output, 0, status), prefix, maxEntries, maxBytes);
}
export function transferProgram402V2(seed: StaticArray<u8>, source: AccountId, asset: AssetId, recipient: AccountId, amount: Amount): i32 {
  if (seed.length > MAX_PROGRAM_ACCOUNT_SEED_BYTES) return ERR_BOUNDS;
  if (amount.isZero()) return ERR_ZERO_AMOUNT;
  const sourceBytes = source.bytes; const assetBytes = asset.bytes; const recipientBytes = recipient.bytes;
  return exactVersioned(v2TransferProgram402(amount.highWord(), amount.lowWord(), pointer(seed), seed.length,
    pointer(sourceBytes), 32, pointer(assetBytes), 32, pointer(recipientBytes), 32), 0);
}
export function fundProgram402V2(seed: StaticArray<u8>, destination: AccountId, asset: AssetId, amount: Amount): i32 {
  if (seed.length > MAX_PROGRAM_ACCOUNT_SEED_BYTES) return ERR_BOUNDS;
  if (amount.isZero()) return ERR_ZERO_AMOUNT;
  const destinationBytes = destination.bytes; const assetBytes = asset.bytes;
  return exactVersioned(v2FundProgram402(amount.highWord(), amount.lowWord(), pointer(seed), seed.length, pointer(destinationBytes), 32, pointer(assetBytes), 32), 0);
}
export function readContextV2(field: i32): VersionedBytes {
  let width = 0;
  if (field == 1 || field == 3) width = 32;
  else if (field == 2) width = 33;
  else if (field == 4 || field == 5 || field == 8) width = 8;
  else if (field == 6 || field == 7) width = 2;
  else if (field == 9) width = 4;
  else return failed(ERR_INVALID);
  const output = new StaticArray<u8>(width);
  const status = v2ContextRead(field, pointer(output), width);
  if (status < 0) return failed(status);
  if (field == 2) {
    if (status == 1 && output[0] == 0) return new VersionedBytes(OK, false, slice(output, 0, 1));
    if (status != 33 || output[0] != 1 || isZero(slice(output, 1, 32))) return failed(ERR_INVALID);
  } else if (status != width || ((field == 1 || field == 3) && isZero(output))) return failed(ERR_INVALID);
  return new VersionedBytes(OK, true, slice(output, 0, status));
}
export function readBalanceV2(account: AccountId, asset: AssetId): VersionedBytes {
  const accountBytes = account.bytes; const assetBytes = asset.bytes; const output = new StaticArray<u8>(16);
  return initialized(v2BalanceRead(pointer(accountBytes), 32, pointer(assetBytes), 32, pointer(output), 16), output, 16);
}
export function hashV2(algorithm: i32, input: StaticArray<u8>): VersionedBytes {
  if (algorithm < 1 || algorithm > 3) return failed(ERR_INVALID);
  if (input.length > 1048576) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(32);
  const status = exactVersioned(v2Hash(algorithm, pointer(input), input.length, pointer(output)), 0);
  return status == OK ? new VersionedBytes(OK, true, output) : failed(status);
}

export function verifyEd25519V2(message: StaticArray<u8>, key: StaticArray<u8>, signature: StaticArray<u8>): i32 {
  if (message.length > 64 || key.length != 32 || signature.length != 64) return ERR_BOUNDS;
  return exactVersioned(v2SignatureVerify(1, pointer(message), message.length, pointer(key), 32, pointer(signature), 64), 0);
}
export function verifySecp256k1V2(digest: StaticArray<u8>, key: StaticArray<u8>, signature: StaticArray<u8>): i32 {
  if (digest.length != 32 || (key.length != 33 && key.length != 65) || signature.length != 64) return ERR_BOUNDS;
  return exactVersioned(v2SignatureVerify(2, pointer(digest), 32, pointer(key), key.length, pointer(signature), 64), 0);
}
export function recoverSecp256k1V2(digest: StaticArray<u8>, signature: StaticArray<u8>, recoveryId: i32): VersionedBytes {
  if (digest.length != 32 || signature.length != 64 || recoveryId < 0 || recoveryId > 3) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(65);
  return initialized(v2SignatureRecover(pointer(digest), 32, pointer(signature), 64, recoveryId, pointer(output), 65), output, 65);
}
export function bigintMulV2(left: StaticArray<u8>, right: StaticArray<u8>): VersionedBytes {
  if (left.length != 32 || right.length != 32) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(64);
  return initialized(v2BigintMul256(pointer(left), 32, pointer(right), 32, pointer(output), 64), output, 64);
}
export function bigintDivV2(left: StaticArray<u8>, right: StaticArray<u8>): VersionedBytes {
  if (left.length != 32 || right.length != 32) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(32);
  return initialized(v2BigintDiv256(pointer(left), 32, pointer(right), 32, pointer(output), 32), output, 32);
}
export function bigintRemV2(left: StaticArray<u8>, right: StaticArray<u8>): VersionedBytes {
  if (left.length != 32 || right.length != 32) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(32);
  return initialized(v2BigintRem256(pointer(left), 32, pointer(right), 32, pointer(output), 32), output, 32);
}
export function bigintModexpV2(base: StaticArray<u8>, exponent: StaticArray<u8>, modulus: StaticArray<u8>): VersionedBytes {
  if (base.length != 32 || exponent.length != 32 || modulus.length != 32) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(32);
  return initialized(v2BigintModexp256(pointer(base), 32, pointer(exponent), 32, pointer(modulus), 32, pointer(output), 32), output, 32);
}
export function readOracleV3(market: StaticArray<u8>): VersionedBytes {
  if (market.length != 32) return failed(ERR_BOUNDS);
  const output = new StaticArray<u8>(64);
  return initialized(v3OracleRead(pointer(market), 32, pointer(output), 64), output, 64);
}
export function readU64LEVersioned(bytes: StaticArray<u8>, offset: i32): u64 {
  let value: u64 = 0;
  for (let i = 0; i < 8; i++) value |= <u64>bytes[offset + i] << <u64>(8 * i);
  return value;
}
export function readU32LEVersioned(bytes: StaticArray<u8>, offset: i32): u32 {
  let value: u32 = 0;
  for (let i = 0; i < 4; i++) value |= <u32>bytes[offset + i] << <u32>(8 * i);
  return value;
}
export class OracleObservationV3 {
  readonly price: Amount;
  readonly observedAt: u64;
  readonly sequence: u64;
  readonly sourceSetDigest: StaticArray<u8>;
  constructor(price: Amount, observedAt: u64, sequence: u64, digest: StaticArray<u8>) {
    this.price = price; this.observedAt = observedAt; this.sequence = sequence; this.sourceSetDigest = digest;
  }
  static decode(record: StaticArray<u8>): OracleObservationV3 | null {
    if (record.length != 64) return null;
    return new OracleObservationV3(Amount.fromParts(readU64LEVersioned(record, 8), readU64LEVersioned(record, 0)),
      readU64LEVersioned(record, 16), readU64LEVersioned(record, 24), slice(record, 32, 32));
  }
}
export const WEB_ANSWER_HEADER_BYTES: i32 = 40;
export const MAX_WEB_RESPONSE_BYTES: i32 = 4096;
export const WEB_RECORD_BYTES: i32 = 4136;
export class WebAnswerV4 {
  readonly contentDigest: StaticArray<u8>;
  readonly fullLength: u32;
  readonly response: StaticArray<u8>;
  constructor(digest: StaticArray<u8>, fullLength: u32, response: StaticArray<u8>) {
    this.contentDigest = digest; this.fullLength = fullLength; this.response = response;
  }
  isTruncated(): bool { return <u32>this.response.length < this.fullLength; }
  static decode(record: StaticArray<u8>): WebAnswerV4 | null {
    if (record.length < WEB_ANSWER_HEADER_BYTES || record.length > WEB_RECORD_BYTES) return null;
    const full = readU32LEVersioned(record, 32);
    const returned = readU32LEVersioned(record, 36);
    if (returned > full || returned != <u32>(record.length - 40) || returned > <u32>MAX_WEB_RESPONSE_BYTES) return null;
    return new WebAnswerV4(slice(record, 0, 32), full, slice(record, 40, <i32>returned));
  }
}
export function readWebV4(requestId: u64): VersionedBytes {
  const request = new StaticArray<u8>(8);
  for (let i = 0; i < 8; i++) request[i] = <u8>(requestId >> <u64>(8 * i));
  const output = new StaticArray<u8>(WEB_RECORD_BYTES);
  const status = v4WebRead(pointer(request), 8, pointer(output), WEB_RECORD_BYTES);
  if (status == STATUS_WEB_ABSENT) return new VersionedBytes(OK, false, new StaticArray<u8>(0));
  if (status < 0) return failed(status);
  if (status < WEB_ANSWER_HEADER_BYTES || status > WEB_RECORD_BYTES) return failed(ERR_INVALID);
  const record = slice(output, 0, status);
  if (WebAnswerV4.decode(record) === null) return failed(ERR_INVALID);
  return new VersionedBytes(OK, true, record);
}
