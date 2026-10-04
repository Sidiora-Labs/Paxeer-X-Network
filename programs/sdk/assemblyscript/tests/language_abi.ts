import {
  AccountId, Amount, AssetId, ProgramId, OK, ERR_INVALID, ERR_BOUNDS,
  fromString, slice, reserveCallInput, acceptCallInput, callInputBytes, emitEvent,
  STORAGE_PRINCIPAL, writeScopedV2, publishResponseV2, publishRefusalV2,
  V2_REFUSAL_SENTINEL, readOracleV3, readWebV4, callProgramResponseV2, fundProgram402V2,
  decodeCallResponseV2, decodeScanPageV2, WebAnswerV4, OracleObservationV3,
  hashV2, verifyEd25519V2, bigintMulV2, readContextV2, admitAbiUpgrade,
  capabilityEncodingVersion, abiManifest, abiManifestV2, abiManifestV3, abiManifestV4
} from "../assembly/index";

function binding(): StaticArray<u8> { return fromString("binding"); }
function publish(bytes: StaticArray<u8>): i32 {
  const status = publishResponseV2(7, bytes);
  return status == OK ? 7 : status;
}
function run(input: StaticArray<u8>): i32 {
  if (input.length == 0) return ERR_INVALID;
  const operation = input[0];
  if (operation == 0) {
    let status = writeScopedV2(STORAGE_PRINCIPAL, fromString("key"), binding());
    if (status != OK) return status;
    status = emitEvent(fromString("topic"), binding());
    return status == OK ? publish(binding()) : status;
  }
  if (operation == 1) {
    const market = new StaticArray<u8>(32);
    for (let i = 0; i < 32; i++) market[i] = 0x11;
    const observation = readOracleV3(market);
    return observation.ok() ? publish(observation.bytes) : observation.status;
  }
  if (operation == 2) {
    const answer = readWebV4(<u64>0x0102030405060708);
    if (!answer.ok()) return answer.status;
    if (!answer.found) return ERR_INVALID;
    return publish(answer.bytes);
  }
  if (operation == 3) {
    const status = publishRefusalV2(1, fromString("no"));
    return status == OK ? V2_REFUSAL_SENTINEL : status;
  }
  if (operation == 4) {
    const id = new StaticArray<u8>(32);
    for (let i = 0; i < 32; i++) id[i] = 0x72;
    const callee = ProgramId.fromBytes(id, 0);
    if (callee === null) return ERR_INVALID;
    const nested = new StaticArray<u8>(1); nested[0] = 6;
    const response = callProgramResponseV2(changetype<ProgramId>(callee), nested,
      new StaticArray<u8>(2), new StaticArray<u8>(1048576));
    if (!response.ok()) return response.status;
    const status = publishResponseV2(response.code, response.bytes);
    return status == OK ? response.code : status;
  }
  if (operation == 5) {
    if (input.length != 33) return ERR_INVALID;
    const destination = AccountId.fromBytes(input, 1);
    const assetBytes = new StaticArray<u8>(32); assetBytes[0] = 9;
    const asset = AssetId.fromBytes(assetBytes, 0);
    if (destination === null || asset === null) return ERR_INVALID;
    const status = fundProgram402V2(fromString("fixture"), changetype<AccountId>(destination),
      changetype<AssetId>(asset), Amount.fromU64(1));
    return status == OK ? publish(binding()) : status;
  }
  if (operation == 6) return publish(binding());
  if (operation == 7) return writeScopedV2(STORAGE_PRINCIPAL, fromString("key"), binding());
  return ERR_INVALID;
}

export function layerx_reserve(length: i32): i32 { return reserveCallInput(length); }
export function layerx_call(inputPointer: i32, inputLength: i32): i32 {
  const admitted = acceptCallInput(inputPointer, inputLength);
  return admitted < 0 ? admitted : run(callInputBytes(admitted));
}
export function layerx_main(selector: i64): i64 {
  if (selector < 0 || selector > 7) return <i64>ERR_INVALID;
  const input = new StaticArray<u8>(1); input[0] = <u8>selector;
  return <i64>run(input);
}

export function languageAbiBounds(): bool {
  if (admitAbiUpgrade(4, 3) || admitAbiUpgrade(0, 1) || admitAbiUpgrade(1, 5)) return false;
  if (!admitAbiUpgrade(1, 4) || capabilityEncodingVersion(1) != 1 || capabilityEncodingVersion(4) != 2) return false;
  if (publishResponseV2(-1, new StaticArray<u8>(0)) != ERR_INVALID || publishRefusalV2(254, new StaticArray<u8>(0)) != ERR_INVALID) return false;
  if (hashV2(4, new StaticArray<u8>(0)).status != ERR_INVALID) return false;
  if (verifyEd25519V2(new StaticArray<u8>(65), new StaticArray<u8>(32), new StaticArray<u8>(64)) != ERR_BOUNDS) return false;
  if (bigintMulV2(new StaticArray<u8>(31), new StaticArray<u8>(32)).status != ERR_BOUNDS) return false;
  if (readContextV2(10).status != ERR_INVALID) return false;
  const output = new StaticArray<u8>(4);
  if (decodeCallResponseV2((<i64>7 << 32) | 5, output).ok()) return false;
  if (decodeCallResponseV2(-6, output).status != -6) return false;
  const emptyPage = new StaticArray<u8>(5);
  if (!decodeScanPageV2(emptyPage, new StaticArray<u8>(0), 1, 5).ok()) return false;
  emptyPage[2] = 1;
  if (decodeScanPageV2(emptyPage, new StaticArray<u8>(0), 1, 5).ok()) return false;
  if (WebAnswerV4.decode(new StaticArray<u8>(39)) !== null || OracleObservationV3.decode(new StaticArray<u8>(63)) !== null) return false;
  return abiManifest().length < abiManifestV2().length && abiManifestV2().length < abiManifestV3().length && abiManifestV3().length < abiManifestV4().length;
}
