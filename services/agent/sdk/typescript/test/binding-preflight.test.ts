import assert from "node:assert/strict";
import { test } from "node:test";

import { ed25519 } from "@noble/curves/ed25519.js";
import { secp256k1 } from "@noble/curves/secp256k1.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import {
  ADDR_PRECOMPILE_ADDRESS,
  AccountDerivationError,
  SPOT_PRECOMPILES,
  TradingWriter,
  bindNonceCall,
  bindTransactionRequest,
  boundDidCall,
  spotCall,
  deriveFromSeed,
  layerXBindMessage,
  layerXDidCall,
  planLayerXBind,
  readLayerXBindState,
  type BindFees,
  type Eip1193Requester,
  type PrecompileCall,
} from "../src/index.js";
import {
  AiMarketError,
  FUND,
  FUNDING_POLICY_VERSION,
  encodeFundRequest,
  prepareOperation,
  snapshotIdOf,
  type NativeTerms,
  type SnapshotBinding,
} from "../src/ai_market.js";
import type { NativePrepareResultV1 } from "../src/generated/client.js";

const CHAIN_ID = 125n;
const hex = (bytes: Uint8Array): string => Buffer.from(bytes).toString("hex");
const word = (value: bigint): string => value.toString(16).padStart(64, "0");
const id = (byte: number): string =>
  byte.toString(16).padStart(2, "0").repeat(32);

/**
 * An EIP-1193 endpoint holding `addr` precompile bindings. Reads answer the
 * three selectors readLayerXBindState sends; a bind transaction is admitted
 * only with a valid Ed25519 signature over the bind message at the current
 * nonce; every request method is logged.
 */
class AddrChain implements Eip1193Requester {
  readonly methods: string[] = [];
  readonly bound = new Map<string, string>();
  readonly nonces = new Map<string, bigint>();
  evmNonce = 0n;

  bind(address: string, did: string): void {
    this.bound.set(address.toLowerCase(), did);
    this.nonces.set(address.toLowerCase(), 1n);
  }

  async request({
    method,
    params,
  }: Readonly<{
    method: string;
    params?: readonly unknown[];
  }>): Promise<unknown> {
    this.methods.push(method);
    const first = params?.[0];
    switch (method) {
      case "eth_call": {
        const call = first as { to: string; data: string };
        assert.equal(call.to, ADDR_PRECOMPILE_ADDRESS);
        assert.equal(params?.[1], "latest");
        const address = `0x${call.data.slice(-40)}`;
        const did = this.bound.get(address);
        if (call.data === boundDidCall(address)) {
          return `0x${"00".repeat(64)}${did ?? "00".repeat(32)}${"00".repeat(32)}`;
        }
        if (call.data === bindNonceCall(address)) {
          return `0x${word(this.nonces.get(address) ?? 0n)}`;
        }
        if (call.data === layerXDidCall(address) && did !== undefined) {
          const text = Buffer.from(`did:layerx:${did}`, "ascii");
          return `0x${did}${word(64n)}${word(BigInt(text.length))}${hex(Buffer.concat([text, Buffer.alloc(96 - text.length)]))}`;
        }
        throw new Error(`unexpected addr read ${call.data}`);
      }
      case "eth_sendTransaction": {
        const transaction = first as {
          from: string;
          to: string;
          data: string;
          value: string;
        };
        assert.equal(transaction.to, ADDR_PRECOMPILE_ADDRESS);
        assert.equal(transaction.value, "0x0");
        const address = transaction.from.toLowerCase();
        assert.equal(this.bound.has(address), false);
        const did = transaction.data.slice(10, 74);
        const signature = Buffer.from(transaction.data.slice(-128), "hex");
        const nonce = this.nonces.get(address) ?? 0n;
        assert.equal(
          ed25519.verify(
            signature,
            layerXBindMessage(CHAIN_ID, transaction.from, nonce),
            Buffer.from(did, "hex"),
          ),
          true,
        );
        this.bound.set(address, did);
        this.nonces.set(address, nonce + 1n);
        return `0x${hex(keccak_256(Buffer.from(transaction.data.slice(2), "hex")))}`;
      }
      case "eth_getTransactionCount":
        if (params?.[1] === "pending") {
          throw Object.assign(new Error("pending block is not available"), {
            code: -32000,
          });
        }
        assert.equal(params?.[1], "latest");
        return `0x${this.evmNonce.toString(16)}`;
      case "eth_sendRawTransaction": {
        assert.equal(typeof first, "string");
        this.evmNonce += 1n;
        return `0x${hex(keccak_256(Buffer.from((first as string).slice(2), "hex")))}`;
      }
      default:
        throw new Error(`unexpected method ${method}`);
    }
  }
}

const account = deriveFromSeed(
  Buffer.from("000102030405060708090a0b0c0d0e0f", "hex"),
);
const foreignDid = hex(
  ed25519.getPublicKey(
    Uint8Array.from({ length: 32 }, (_value, index) => 0x40 + index),
  ),
);
const fees: BindFees = {
  evmNonce: 0n,
  maxPriorityFeePerGas: 1n,
  maxFeePerGas: 9n,
  gasLimit: 80_000n,
};
const call: PrecompileCall = spotCall(
  "pecor",
  "marketBuy(address,address,uint256,uint256,uint256)",
  [`0x${"31".repeat(20)}`, `0x${"32".repeat(20)}`, 1000n, 900n, 1700000000n],
);

/** Counts TradingWriter.transactionRequest, the only place TradingWriter.send creates a secp256k1 signature. */
async function countingSignatures<T>(
  body: () => Promise<T>,
): Promise<{ result: T | Error; signatures: number }> {
  const original = TradingWriter.transactionRequest;
  let signatures = 0;
  TradingWriter.transactionRequest = (...args) => {
    signatures += 1;
    return original(...args);
  };
  try {
    return { result: await body(), signatures };
  } catch (error) {
    assert.ok(error instanceof Error);
    return { result: error, signatures };
  } finally {
    TradingWriter.transactionRequest = original;
  }
}

function refusedWith(result: unknown, code: string): void {
  assert.ok(
    result instanceof AccountDerivationError,
    `expected ${code}, got ${String(result)}`,
  );
  assert.equal(result.code, code);
}

test("TradingWriter.send refuses an unbound address before signing or sending", async () => {
  const chain = new AddrChain();
  const { result, signatures } = await countingSignatures(() =>
    TradingWriter.send(chain, account, CHAIN_ID, call, fees),
  );
  refusedWith(result, "address_unbound");
  assert.equal(signatures, 0);
  assert.deepEqual(chain.methods, ["eth_call", "eth_call"]);
});

test("TradingWriter.send refuses an address bound to a different DID before signing or sending", async () => {
  const chain = new AddrChain();
  chain.bind(account.evmAddress, foreignDid);
  const { result, signatures } = await countingSignatures(() =>
    TradingWriter.send(chain, account, CHAIN_ID, call, fees),
  );
  refusedWith(result, "bound_to_different_did");
  assert.equal(signatures, 0);
  assert.deepEqual(chain.methods, ["eth_call", "eth_call", "eth_call"]);
});

test("TradingWriter.send signs and sends for the bound pair", async () => {
  const chain = new AddrChain();
  chain.bind(account.evmAddress, account.layerxPublicKey);
  const expected = TradingWriter.transactionRequest(
    account,
    CHAIN_ID,
    call,
    fees,
  );
  const { result, signatures } = await countingSignatures(() =>
    TradingWriter.send(chain, account, CHAIN_ID, call, fees),
  );
  assert.equal(signatures, 1);
  assert.equal(
    result,
    `0x${hex(keccak_256(Buffer.from((expected.params[0] as string).slice(2), "hex")))}`,
  );
  assert.equal(call.to, SPOT_PRECOMPILES.pecor);
  assert.deepEqual(chain.methods, [
    "eth_call",
    "eth_call",
    "eth_call",
    "eth_getTransactionCount",
    "eth_sendRawTransaction",
  ]);
});

test("an unbound account binds through the explicit bind path and then writes through TradingWriter.send", async () => {
  const chain = new AddrChain();
  const plan = planLayerXBind(
    account,
    CHAIN_ID,
    await readLayerXBindState(chain, account.evmAddress),
  );
  assert.equal(plan.action, "bind");
  assert.ok(plan.action === "bind");
  assert.equal(plan.call.nonce, 0n);
  const bindHash = await chain.request(
    bindTransactionRequest(account, plan.call),
  );
  assert.match(String(bindHash), /^0x[0-9a-f]{64}$/u);
  assert.deepEqual(await readLayerXBindState(chain, account.evmAddress), {
    boundDidPublicKey: account.layerxPublicKey,
    nonce: 1n,
  });
  chain.methods.length = 0;
  const { result, signatures } = await countingSignatures(() =>
    TradingWriter.send(chain, account, CHAIN_ID, call, fees),
  );
  assert.equal(signatures, 1);
  assert.match(String(result), /^0x[0-9a-f]{64}$/u);
  assert.deepEqual(chain.methods.slice(-2), [
    "eth_getTransactionCount",
    "eth_sendRawTransaction",
  ]);
});

// ----- Approval.sign ---------------------------------------------------------------------------

const ownerSecret = Uint8Array.from(
  { length: 32 },
  (_value, index) => index + 1,
);
const ownerKey = secp256k1.getPublicKey(ownerSecret, true);
const ownerAddress = `0x${hex(keccak_256(secp256k1.getPublicKey(ownerSecret, false).subarray(1)).subarray(12))}`;
const ownerDid = hex(
  ed25519.getPublicKey(
    Uint8Array.from({ length: 32 }, (_value, index) => 0x60 + index),
  ),
);

const binding: SnapshotBinding = {
  chain: id(0x01),
  program: id(0x02),
  market: id(0x03),
  observedSequence: 1_000n,
  executionHeight: 500n,
  batchId: id(0x04),
  nativeStateRoot: id(0x05),
  revision: 7n,
  stateDigest: id(0x06),
  epoch: 3n,
  config: 2n,
  policy: id(0x07),
  roster: id(0x08),
  checkpoint: id(0x09),
  settlement: null,
  rank: 4,
  publicationTimeMs: 1_700_000_000_000n,
};
const terms: NativeTerms = {
  networkId: 125,
  actorDid: "did:layerx:market",
  ownerPublicKey: ownerKey,
  accountSequence: 4n,
  idempotencyKey: id(0xd0),
  notBefore: 0n,
  notAfter: 900n,
  gasLimit: 1_000_000n,
  maxFeePerGas: 67_000_000n,
  maxPriorityFeePerGas: 1n,
  feeLimit: 1_000n,
  capabilities: new Uint8Array(),
  accessDeclaration: new Uint8Array(),
  responseCapacity: 4_096,
  resources: [
    1_000_000n,
    16_777_216n,
    1_048_576n,
    1_048_576n,
    64n,
    1_048_576n,
    4_096n,
  ],
};

function approval() {
  const prepared = prepareOperation(
    { binding, snapshotId: snapshotIdOf(binding) },
    500n,
    {
      selector: FUND,
      payload: encodeFundRequest({
        amount: 5n,
        refundRecipient: id(0xb0),
        policyVersion: FUNDING_POLICY_VERSION,
        consent: true,
      }),
      actor: id(0xc0),
      roster: "bound",
      sequence: 1n,
      expiry: 900n,
      request: id(0xd0),
      requiredRank: 4,
    },
    terms,
  );
  const canonical = prepared.canonicalBytes();
  const disclosure: NativePrepareResultV1 = {
    version: "1",
    preparation_id: prepared.preparationId,
    canonical_bytes: hex(canonical),
    signing_preimage: hex(keccak_256(canonical)),
    activity: { version: "1", module: "9", ordinal: "3" },
    approval_required: false,
    approval_id: null,
  };
  const review = prepared.review(disclosure);
  return { prepared, approved: review.approve(review.terms, ownerKey) };
}

function countingOwner() {
  const signer = {
    calls: 0,
    publicKey: ownerKey,
    sign(preimage: Uint8Array): Uint8Array {
      signer.calls += 1;
      const signature = secp256k1.sign(preimage, ownerSecret, {
        prehash: false,
        format: "recovered",
      });
      return new Uint8Array([...signature.slice(1), 27 + (signature[0] ?? 0)]);
    },
  };
  return signer;
}

async function refusal(action: () => Promise<unknown>): Promise<unknown> {
  try {
    await action();
  } catch (error) {
    return error;
  }
  return assert.fail("expected a refusal");
}

test("Approval.sign refuses an unbound approving key before the signer is invoked", async () => {
  const chain = new AddrChain();
  const signer = countingOwner();
  refusedWith(
    await refusal(() =>
      approval().approved.sign(signer, 500n, chain, ownerDid),
    ),
    "address_unbound",
  );
  assert.equal(signer.calls, 0);
  assert.deepEqual(chain.methods, ["eth_call", "eth_call"]);
});

test("Approval.sign refuses an approving key bound to a different DID before the signer is invoked", async () => {
  const chain = new AddrChain();
  chain.bind(ownerAddress, foreignDid);
  const signer = countingOwner();
  refusedWith(
    await refusal(() =>
      approval().approved.sign(signer, 500n, chain, ownerDid),
    ),
    "bound_to_different_did",
  );
  assert.equal(signer.calls, 0);
  assert.equal(chain.methods.includes("eth_getTransactionCount"), false);
  assert.equal(chain.methods.includes("eth_sendRawTransaction"), false);
});

test("Approval.sign keeps the freshness and key checks ahead of the binding read", async () => {
  const chain = new AddrChain();
  const signer = countingOwner();
  const stale = await refusal(() =>
    approval().approved.sign(signer, 509n, chain, ownerDid),
  );
  assert.ok(stale instanceof AiMarketError);
  assert.equal(stale.code, "StaleAuthority");
  assert.deepEqual(chain.methods, []);
  assert.equal(signer.calls, 0);
});

test("Approval.sign signs once for the key bound to the expected DID", async () => {
  const chain = new AddrChain();
  chain.bind(ownerAddress, ownerDid);
  const signer = countingOwner();
  const { prepared, approved } = approval();
  const record = await approved.sign(
    signer,
    500n,
    chain,
    ownerDid.toUpperCase(),
  );
  assert.equal(signer.calls, 1);
  assert.equal(record.state, "signed");
  assert.equal(record.intent, prepared.intent);
  assert.deepEqual(chain.methods, ["eth_call", "eth_call", "eth_call"]);
});
