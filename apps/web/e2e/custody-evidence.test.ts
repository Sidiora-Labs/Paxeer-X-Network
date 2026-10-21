import assert from "node:assert/strict";
import test from "node:test";
import { createHash, randomUUID } from "node:crypto";
import { closeSync, fsyncSync, lstatSync, openSync, readFileSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import { join, resolve, sep } from "node:path";

import {
  PaxeerProvider,
  decodeCustodyAuthorization,
  encodeCustodyAuthorization,
  type CustodyAuthorization,
} from "@paxeer/wallet/provider";
import { encodeFunctionData, recoverMessageAddress, type Hex } from "viem";

import type { JourneyStage, WalletSignRequest } from "../src/api/index.ts";
import {
  browserWalletBridge,
  custodyFromWalletSignRequest,
  custodyHandoffStorageKey,
} from "../src/journeys/custody/handoff.ts";
import {
  presentedStageState,
  stageEvidenceBacked,
  stageEvidenceRule,
} from "../src/journeys/custody/evidence.ts";

test("unknown custody stages fail closed even with receipt evidence", () => {
  const stage: JourneyStage = {
    stage_id: "future-stage",
    copy_key: "withdraw.stage.future-payout",
    state: "done-finalised",
    evidence: [{
      evidence_id: "receipt-1",
      class: "layerx-receipt",
      verification: "paxeer-finalised",
    }],
  };

  assert.equal(stageEvidenceRule(stage.copy_key), undefined);
  assert.equal(stageEvidenceBacked(stage), false);
  assert.equal(presentedStageState(stage), "still-checking");
});

const depositAbi = [{ type: "function", name: "depositToken", stateMutability: "nonpayable", inputs: [
  { name: "pointer", type: "address" }, { name: "amount", type: "uint256" }, { name: "beneficiary", type: "bytes32" },
], outputs: [{ name: "depositId", type: "bytes32" }] }] as const;
const nativeDepositAbi = [{ type: "function", name: "deposit", stateMutability: "payable", inputs: [
  { name: "beneficiary", type: "bytes32" },
], outputs: [{ name: "depositId", type: "bytes32" }] }] as const;
const account = "0x0000000000000000000000000000000000000011" as Hex;
const custodyTo = "0x0000000000000000000000000000000000001013" as Hex;
const pointer = "0x0000000000000000000000000000000000000012" as Hex;
const beneficiary = `0x${"21".repeat(32)}` as Hex;
function canonicalAuthorization(): CustodyAuthorization {
  return {
    account, chainId: 125n, to: custodyTo, value: 0n,
    data: encodeFunctionData({ abi: depositAbi, functionName: "depositToken", args: [pointer, 1n, beneficiary] }),
    nonce: 1n, deadline: BigInt(Math.floor(Date.now() / 1000) + 300),
    gas: 250000n, maxFeePerGas: 100n, maxPriorityFeePerGas: 1n,
  };
}
function requestFor(custody: Hex, from = account, stage = "deposit-wallet"): WalletSignRequest {
  return {
    stage_id: stage, copy_key: "deposit.stage.waiting-for-wallet", from_address: from,
    to_sign_base64: Buffer.from(custody.slice(2), "hex").toString("base64"), settlement_domain: "paxeer",
  };
}

test("private wallet request preserves the genuine v2 custody authorization bytes", () => {
  const authorization = canonicalAuthorization();
  const custody = encodeCustodyAuthorization(authorization);
  const converted = custodyFromWalletSignRequest(requestFor(custody));
  assert.equal(converted.custody, custody.toLowerCase());
  assert.deepEqual(converted.authorization, decodeCustodyAuthorization(custody));
  assert.equal(encodeCustodyAuthorization(converted.authorization), custody);
  assert.equal(converted.authorization.data, authorization.data);
  assert.equal(converted.authorization.nonce, authorization.nonce);
  assert.equal(converted.authorization.maxFeePerGas, authorization.maxFeePerGas);
});

test("private custody refuses malformed transport and foreign authority before wallet submission", () => {
  const custody = encodeCustodyAuthorization(canonicalAuthorization());
  const request = requestFor(custody);
  const nativeCustody = encodeCustodyAuthorization({ ...canonicalAuthorization(), value: 1n,
    data: encodeFunctionData({ abi: nativeDepositAbi, functionName: "deposit", args: [beneficiary] }) });
  const nativeRequest = requestFor(nativeCustody);
  assert.equal(custodyFromWalletSignRequest(nativeRequest).custody, nativeCustody);
  assert(request.to_sign_base64.endsWith("=="));
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  const lastIndex = request.to_sign_base64.length - 3;
  const padBits = request.to_sign_base64.slice(0, lastIndex)
    + alphabet[alphabet.indexOf(request.to_sign_base64[lastIndex]!) | 1] + "==";
  assert.equal(Buffer.from(padBits, "base64").toString("hex"), custody.slice(2));
  for (const to_sign_base64 of ["", "not-base64!", request.to_sign_base64 + "\n", request.to_sign_base64.replace(/=+$/, ""),
    nativeRequest.to_sign_base64 + "=", padBits,
    Buffer.from(custody.slice(2) + "00", "hex").toString("base64"),
    Buffer.from("LX:CUSTODY:v1").toString("base64")]) {
    assert.throws(() => custodyFromWalletSignRequest({ ...request, to_sign_base64 }));
  }
  assert.throws(() => custodyFromWalletSignRequest({ ...request, from_address: pointer }));
  assert.throws(() => custodyFromWalletSignRequest(request, 126n));
  assert.throws(() => custodyFromWalletSignRequest({ ...request, settlement_domain: "layerx" } as unknown as WalletSignRequest));
  const { settlement_domain: _domain, ...withoutDomain } = request;
  assert.equal(custodyFromWalletSignRequest(withoutDomain).custody, custody);
});

test("genuine custody owner rejects zero beneficiary and modified deposit construction", () => {
  const authorization = canonicalAuthorization();
  assert.throws(() => encodeCustodyAuthorization({ ...authorization, data: encodeFunctionData({
    abi: depositAbi, functionName: "depositToken", args: [pointer, 1n, `0x${"00".repeat(32)}`],
  }) }));
  assert.throws(() => encodeCustodyAuthorization({ ...authorization, data: `${authorization.data}00` }));
  assert.throws(() => encodeCustodyAuthorization({ ...authorization, to: pointer }));
  assert.throws(() => encodeCustodyAuthorization({ ...authorization, value: 1n }));
  assert.throws(() => encodeCustodyAuthorization({ ...authorization, maxPriorityFeePerGas: 101n }));
  assert.throws(() => decodeCustodyAuthorization(`${encodeCustodyAuthorization(authorization)}00`));
});

test("private browser handoff reports a missing installed wallet without approval", async () => {
  const outcome = await browserWalletBridge(() => undefined).sign(requestFor(encodeCustodyAuthorization(canonicalAuthorization())));
  assert.deepEqual(outcome, { outcome: "unavailable" });
});

interface PrivateRuntime {
  version: number; isolated: boolean; approved_custody_execution: boolean;
  gateway_url: string; rpc_url: string; real_rpc_url: string; control_url: string;
  fixture_dir: string; evidence_dir: string; owner_token_file: string;
  account: Hex; custody_beneficiary: Hex; chain_id: number; custody_pointer: Hex; custody_amount: string;
}

function privateFile(path: string): string {
  const info = lstatSync(path);
  assert(info.isFile() && !info.isSymbolicLink() && info.nlink === 1 && (info.mode & 0o077) === 0);
  assert.equal(info.uid, process.getuid!());
  return readFileSync(path, "utf8");
}

if (process.env.PRIVATE_HUMAN_CUSTODY_RUNTIME !== undefined) {
  test("private Human handoff uses real custody quorum, retained proof, restart and receipt evidence", { timeout: 360000 }, async () => {
    const runtime = JSON.parse(privateFile(process.env.PRIVATE_HUMAN_CUSTODY_RUNTIME!)) as PrivateRuntime;
    assert.equal(runtime.version, 1);
    assert.equal(runtime.isolated, true);
    assert.equal(runtime.approved_custody_execution, true);
    assert.equal(runtime.chain_id, 125);
    assert(/^0x[0-9a-f]{64}$/.test(runtime.custody_beneficiary) && !/^0x0{64}$/.test(runtime.custody_beneficiary));
    for (const endpoint of [runtime.gateway_url, runtime.rpc_url, runtime.real_rpc_url, runtime.control_url]) {
      assert(["127.0.0.1", "localhost", "[::1]"].includes(new URL(endpoint).hostname), "only supplied disposable loopback services may be used");
    }
    const material = resolve(runtime.fixture_dir, runtime.owner_token_file);
    assert(material.startsWith(resolve(runtime.fixture_dir) + sep));
    const token = privateFile(material).trim();
    const directory = lstatSync(runtime.evidence_dir);
    assert(directory.isDirectory() && !directory.isSymbolicLink() && (directory.mode & 0o077) === 0);
    assert.equal(directory.uid, process.getuid!());
    const storage = {
      getItem(key: string): string | null {
        const path = join(runtime.evidence_dir, "human-custody-" + createHash("sha256").update(key).digest("hex") + ".json");
        try { return privateFile(path); } catch (error) {
          if ((error as NodeJS.ErrnoException).code === "ENOENT") return null;
          throw error;
        }
      },
      setItem(key: string, value: string): void {
        const path = join(runtime.evidence_dir, "human-custody-" + createHash("sha256").update(key).digest("hex") + ".json");
        const temporary = path + "." + randomUUID();
        const fd = openSync(temporary, "wx", 0o600);
        try { writeFileSync(fd, value); fsyncSync(fd); } finally { closeSync(fd); }
        renameSync(temporary, path);
        const dir = openSync(runtime.evidence_dir, "r");
        try { fsyncSync(dir); } finally { closeSync(dir); }
      },
      removeItem(key: string): void {
        const path = join(runtime.evidence_dir, "human-custody-" + createHash("sha256").update(key).digest("hex") + ".json");
        try { privateFile(path); unlinkSync(path); } catch (error) {
          if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
        }
        const dir = openSync(runtime.evidence_dir, "r");
        try { fsyncSync(dir); } finally { closeSync(dir); }
      },
    };
    const provider = (approve = true) => new PaxeerProvider({ gatewayUrl: runtime.gateway_url, rpcUrl: runtime.rpc_url,
      chainId: runtime.chain_id, token: () => token, confirm: () => approve });
    async function control(action: string): Promise<Record<string, unknown>> {
      const response = await fetch(runtime.control_url + "/control/" + action, { method: "POST", body: "{}" });
      assert(response.ok);
      return await response.json() as Record<string, unknown>;
    }
    const original = provider();
    const accounts = await original.request({ method: "eth_requestAccounts" }) as string[];
    assert.equal(accounts[0]!.toLowerCase(), runtime.account.toLowerCase());
    const data = encodeFunctionData({ abi: depositAbi, functionName: "depositToken", args: [
      runtime.custody_pointer, BigInt(runtime.custody_amount), runtime.custody_beneficiary,
    ] });
    const custody = await original.request({ method: "paxeer_prepareCustody", params: [{ account: runtime.account,
      chainId: runtime.chain_id, to: custodyTo, value: "0x0", data }] }) as Hex;
    const request = requestFor(custody, runtime.account, "real-private-custody-" + randomUUID());
    const key = custodyHandoffStorageKey(request);
    const converted = custodyFromWalletSignRequest(request);
    assert.equal(converted.authorization.data.toLowerCase(), data.toLowerCase());
    for (const deadline of [0n, BigInt(Math.floor(Date.now() / 1000) + 3600)]) {
      const invalid = encodeCustodyAuthorization({ ...converted.authorization, deadline });
      await assert.rejects(original.request({ method: "paxeer_signCustody", params: [{ custody: invalid }] }));
    }
    const disconnected = provider();
    disconnected.disconnect();
    assert.deepEqual(await browserWalletBridge(() => disconnected, { storage, expectedChainId: 125n }).sign(request),
      { outcome: "rejected" });
    const refused = provider(false);
    await refused.request({ method: "eth_requestAccounts" });
    assert.deepEqual(await browserWalletBridge(() => refused, { storage, expectedChainId: 125n }).sign({ ...request,
      stage_id: request.stage_id + "-cancelled" }), { outcome: "cancelled" });
    const signature = await original.request({ method: "paxeer_signCustody", params: [{ custody }] }) as Hex;
    assert.equal((await recoverMessageAddress({ message: { raw: custody }, signature })).toLowerCase(), runtime.account.toLowerCase());
    storage.setItem(key, JSON.stringify({ version: 1, stage_id: request.stage_id, account: runtime.account.toLowerCase(),
      chain_id: "125", custody: custody.toLowerCase(), signature: null, phase: "signing", tx_hash: null }));
    await control("restart");
    const recoveredOwner = provider();
    await recoveredOwner.request({ method: "eth_requestAccounts" });
    assert.equal(await recoveredOwner.request({ method: "paxeer_recoverCustody", params: [{ custody }] }), signature);
    assert.equal((JSON.parse(storage.getItem(key)!) as Record<string, unknown>).signature, null);
    await control("arm-custody");
    const restarted = provider();
    await restarted.request({ method: "eth_requestAccounts" });
    const submitted = await browserWalletBridge(() => restarted, { storage, expectedChainId: 125n }).sign(request);
    assert.equal(submitted.outcome, "approved");
    assert("reference" in submitted && /^0x[0-9a-fA-F]{64}$/.test(submitted.reference));
    const retained = JSON.parse(storage.getItem(key)!) as Record<string, unknown>;
    assert.equal(retained.custody, custody.toLowerCase());
    assert.equal(retained.signature, signature.toLowerCase());
    assert.equal(retained.phase, "submitted");
    assert.equal(retained.tx_hash, submitted.reference.toLowerCase());
    assert.equal((await control("state")).dropped_reply, true);
    await control("restart");
    const resumed = provider();
    await resumed.request({ method: "eth_requestAccounts" });
    const repeated = await browserWalletBridge(() => resumed, { storage, expectedChainId: 125n }).sign(request);
    assert.deepEqual(repeated, submitted);
    await control("receipts");
    const end = Date.now() + 90000;
    let status: Record<string, unknown> | undefined;
    while (Date.now() < end) {
      status = await resumed.request({ method: "paxeer_custodyStatus", params: [{ custody }] }) as Record<string, unknown>;
      if (status.status === "confirmed" || status.status === "reverted") break;
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    assert.equal(status?.status, "confirmed");
    assert(status !== undefined);
    assert.equal(status.tx_hash, submitted.reference.toLowerCase());
    assert.equal((status.receipt as Record<string, unknown>).status, "0x1");
    const counts = (await control("state")).send_counts;
    assert(Array.isArray(counts) && counts.length > 0 && counts.every(count => count === 1), "a retained broadcast must not be sent twice");
    const unknownCustody = await resumed.request({ method: "paxeer_prepareCustody", params: [{ account: runtime.account,
      chainId: runtime.chain_id, to: custodyTo, value: "0x0", data }] }) as Hex;
    assert.notEqual(unknownCustody, custody, "a new real nonce must separate unresolved signing from a known transaction");
    const unknownRequest = requestFor(unknownCustody, runtime.account, request.stage_id + "-unknown-signature");
    const unknownKey = custodyHandoffStorageKey(unknownRequest);
    storage.setItem(unknownKey, JSON.stringify({ ...retained, stage_id: unknownRequest.stage_id, custody: unknownCustody,
      signature: null, phase: "signing", tx_hash: null }));
    const unknown = await browserWalletBridge(() => resumed, { storage, expectedChainId: 125n }).sign(unknownRequest);
    assert.equal(unknown.outcome, "failed");
    assert.equal((JSON.parse(storage.getItem(unknownKey)!) as Record<string, unknown>).signature, null);
    for (const changed of [{ ...retained, chain_id: "126" }, { ...retained, account: pointer },
      { ...retained, custody: unknownCustody }, { ...retained, tx_hash: `0x${"00".repeat(32)}` }]) {
      storage.setItem(key, JSON.stringify(changed));
      assert.deepEqual(await browserWalletBridge(() => resumed, { storage, expectedChainId: 125n }).sign(request),
        { outcome: "failed" });
    }
    storage.setItem(key, JSON.stringify(retained));
    await control("lose-quorum");
    const quorumRequest = { ...unknownRequest, stage_id: request.stage_id + "-quorum-unavailable" };
    const unavailable = await browserWalletBridge(() => resumed, { storage, expectedChainId: 125n }).sign(quorumRequest);
    assert.equal(unavailable.outcome, "failed");
    assert.equal((JSON.parse(storage.getItem(custodyHandoffStorageKey(quorumRequest))!) as Record<string, unknown>).phase, "signing");
    writeFileSync(join(runtime.evidence_dir, "private-human-custody-result.json"), JSON.stringify({
      version: 1, cases: ["real-deposit-codec", "owner-confirmation-refusal", "retained-signed-proof", "lost-broadcast-reply",
        "gateway-restart", "same-hash-resume", "single-broadcast", "receipt-confirmation", "unresolved-signature-refusal",
        "original-signature-recovery",
        "expiry-refusal", "disconnected-owner-refusal", "retained-authority-refusal", "real-quorum-unavailable"],
    }, null, 2), { mode: 0o600 });
  });
}
