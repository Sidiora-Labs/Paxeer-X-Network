import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { secp256k1 } from "@noble/curves/secp256k1.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import {
  AccountDerivationError,
  LAYERX_COIN_TYPE,
  autoBindLayerX,
  bindTransactionRequest,
  bindNonceCall,
  boundDidCall,
  decodeLayerXDid,
  decodeBindNonce,
  decodeBoundDid,
  deriveFromBrowserWallet,
  deriveFromMnemonic,
  deriveFromSeed,
  deriveFromWalletSignature,
  keyDerivationHash,
  keyDerivationRequest,
  keyDerivationTypedData,
  normalizeWalletSignature,
  encodeAbiCall,
  layerXBindMessage,
  layerXDidCall,
  planLayerXBind,
  requireLayerXBinding,
  signBindTransaction,
  slip10Ed25519,
  type Eip1193Requester,
} from "../src/index.js";

const fixture = JSON.parse(
  readFileSync(
    new URL("../../../../../platform/sdk/conformance/fixtures/account-derivation-v1.json", import.meta.url),
    "utf8",
  ),
);
const hex = (bytes: Uint8Array): string => Buffer.from(bytes).toString("hex");
const bytes = (text: string): Uint8Array => new Uint8Array(Buffer.from(text, "hex"));
const refuses = (code: string, run: () => unknown): void => {
  assert.throws(run, (error: unknown) => error instanceof AccountDerivationError && error.code === code);
};

// SLIP-0010 official Ed25519 test vector 1.
const slipSeed = bytes("000102030405060708090a0b0c0d0e0f");
assert.equal(hex(slip10Ed25519(slipSeed, [])), "2b4be7f19ee27bbf30c667b642d5f4aa69fd169872f8fc3059c08ebae2eb19e7");
assert.equal(hex(slip10Ed25519(slipSeed, [0, 1])), "b1d0bad404bf35da785a64ca1ac54b2617211d2777696fbffaf208f746ae84f2");
assert.equal(
  hex(slip10Ed25519(slipSeed, [0, 1, 2, 2, 1000000000])),
  "8f94d394a8e8fd6b1bc2f3f49f5c47e385281d5c17e65324b0f62483e37e8793",
);

assert.equal(fixture.layerx_coin_type, LAYERX_COIN_TYPE);
let seen = 0;
for (const vector of fixture.mnemonic_vectors) {
  for (const expected of vector.accounts) {
    const derived = deriveFromMnemonic(`  ${vector.mnemonic.replaceAll(" ", "  ")}\n`, {
      passphrase: vector.passphrase,
      index: expected.index,
    });
    assert.equal(derived.evmAddress, expected.evm_address);
    assert.equal(derived.layerxPublicKey, expected.layerx_public_key);
    assert.equal(derived.did, expected.did);
    const plan = planLayerXBind(derived, expected.bind.chain_id, {
      boundDidPublicKey: null,
      nonce: BigInt(expected.bind.nonce),
    });
    assert.equal(plan.action, "bind");
    if (plan.action === "bind") {
      assert.equal(plan.call.data, `0x${expected.bind.calldata}`);
      assert.equal(plan.call.to, expected.bind.to);
      const transaction = expected.bind.transaction;
      assert.equal(
        signBindTransaction(derived, expected.bind.chain_id, plan.call, {
          evmNonce: BigInt(transaction.evm_nonce),
          maxPriorityFeePerGas: BigInt(transaction.max_priority_fee_per_gas),
          maxFeePerGas: BigInt(transaction.max_fee_per_gas),
          gasLimit: BigInt(transaction.gas_limit),
        }),
        `0x${transaction.raw}`,
      );
    }
    assert.deepEqual(
      planLayerXBind(derived, 713714, { boundDidPublicKey: derived.layerxPublicKey.toUpperCase(), nonce: 1n }),
      {
        action: "already_bound",
      },
    );
    refuses("bound_to_different_did", () =>
      planLayerXBind(derived, 713714, { boundDidPublicKey: "aa".repeat(32), nonce: 1n }),
    );
    seen += 1;
  }
}
assert.equal(seen, 4);
assert.equal(
  deriveFromMnemonic("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about")
    .evmAddress,
  "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
);
refuses("invalid_mnemonic", () => deriveFromMnemonic("abandon abandon abandon"));
refuses("invalid_mnemonic", () => deriveFromMnemonic(`${"abandon ".repeat(11)}abandon`));
refuses("index_out_of_range", () => deriveFromMnemonic(fixture.mnemonic_vectors[0].mnemonic, { index: 0x80000000 }));

const wallet = fixture.wallet_signature;
for (const vector of wallet.vectors) {
  const parameters = { chainId: fixture.chain_id, address: wallet.evm_address, index: vector.index };
  assert.deepEqual(keyDerivationTypedData(parameters), vector.typed_data);
  assert.equal(hex(keyDerivationHash(parameters)), vector.eip712_hash);
  const request = keyDerivationRequest(parameters);
  assert.equal(request.method, "eth_signTypedData_v4");
  assert.equal(request.params[0], wallet.evm_address.toLowerCase());
  assert.deepEqual(JSON.parse(request.params[1] as string), vector.typed_data);

  const produced = secp256k1.sign(keyDerivationHash(parameters), bytes(wallet.private_key), {
    prehash: false,
    format: "recovered",
  });
  assert.equal(hex(produced.slice(1)) + (27 + (produced[0] ?? 0)).toString(16), vector.signature);

  const derived = deriveFromWalletSignature(parameters, `0x${vector.signature}`);
  assert.equal(derived.did, vector.did);
  assert.equal(derived.evmAddress, wallet.evm_address);
  assert.equal(derived.evmPrivateKey, null);
  for (const equivalent of vector.equivalent_signatures) {
    assert.notEqual(equivalent, vector.signature);
    assert.equal(hex(normalizeWalletSignature(equivalent)), vector.signature);
    assert.equal(deriveFromWalletSignature(parameters, bytes(equivalent)).did, vector.did);
  }
  refuses("wallet_signer_mismatch", () =>
    deriveFromWalletSignature({ ...parameters, chainId: fixture.chain_id + 1 }, vector.signature),
  );
  refuses("invalid_wallet_signature", () =>
    deriveFromWalletSignature(parameters, `${vector.signature.slice(0, 128)}1d`),
  );
  refuses("invalid_origin", () => keyDerivationTypedData({ ...parameters, origin: 'https://Evil.example/"' }));
  refuses("evm_key_unavailable", () =>
    signBindTransaction(
      derived,
      713714,
      { to: wallet.evm_address, data: "0x", didPublicKey: "", signature: "", nonce: 0n },
      {
        evmNonce: 0n,
        maxPriorityFeePerGas: 1n,
        maxFeePerGas: 1n,
        gasLimit: 1n,
      },
    ),
  );

  // Browser flow: an in-process RFC 6979 signer holding the test key is asked twice and accepted.
  const asked: string[] = [];
  const deterministic: Eip1193Requester = {
    request: async ({ method, params }) => {
      asked.push(method);
      assert.deepEqual(JSON.parse(params?.[1] as string), vector.typed_data);
      const signature = secp256k1.sign(keyDerivationHash(parameters), bytes(wallet.private_key), {
        prehash: false,
        format: "recovered",
      });
      return `0x${hex(signature.slice(1))}${(27 + (signature[0] ?? 0)).toString(16)}`;
    },
  };
  assert.equal((await deriveFromBrowserWallet(deterministic, parameters)).did, vector.did);
  assert.deepEqual(asked, ["eth_signTypedData_v4", "eth_signTypedData_v4"]);

  // A wallet that signs with a fresh nonce each time is refused.
  let extra = 1n;
  const randomised: Eip1193Requester = {
    request: async () => {
      extra += 1n;
      const signature = secp256k1.sign(keyDerivationHash(parameters), bytes(wallet.private_key), {
        prehash: false,
        format: "recovered",
        extraEntropy: bytes(extra.toString(16).padStart(64, "0")),
      });
      return `0x${hex(signature.slice(1))}${(27 + (signature[0] ?? 0)).toString(16)}`;
    },
  };
  await assert.rejects(
    deriveFromBrowserWallet(randomised, parameters),
    (error: unknown) => error instanceof AccountDerivationError && error.code === "wallet_signature_not_deterministic",
  );
}

// Auto-bind against a wallet answering the two precompile reads.
const derived = deriveFromMnemonic(fixture.mnemonic_vectors[0].mnemonic);
const expected = fixture.mnemonic_vectors[0].accounts[0];
const chain = (bound: string | null, sent: unknown[]): Eip1193Requester => ({
  request: async ({ method, params }) => {
    if (method === "eth_sendTransaction") {
      sent.push(params?.[0]);
      return `0x${"12".repeat(32)}`;
    }
    const call = params?.[0] as { to: string; data: string };
    assert.equal(method, "eth_call");
    assert.equal(call.to, expected.bind.to);
    if (call.data === bindNonceCall(derived.evmAddress)) {
      return `0x${"00".repeat(32)}`;
    }
    if (call.data === layerXDidCall(derived.evmAddress)) {
      assert.notEqual(bound, null);
      return `0x${encodeAbiCall("binding", ["bytes32", "string"], [`0x${bound}`, `did:layerx:${bound}`]).slice(10)}`;
    }
    assert.equal(call.data, boundDidCall(derived.evmAddress));
    return `0x${"00".repeat(64)}${bound ?? "00".repeat(32)}${"00".repeat(32)}`;
  },
});
const sent: unknown[] = [];
const outcome = await autoBindLayerX(chain(null, sent), derived, 713714);
assert.equal(outcome.action, "bind");
assert.deepEqual(sent, [
  { from: derived.evmAddress, to: expected.bind.to, data: `0x${expected.bind.calldata}`, value: "0x0" },
]);
const again: unknown[] = [];
assert.deepEqual(await autoBindLayerX(chain(derived.layerxPublicKey, again), derived, 713714), {
  action: "already_bound",
});
await assert.rejects(
  autoBindLayerX(chain("bb".repeat(32), again), derived, 713714),
  (error: unknown) => error instanceof AccountDerivationError && error.code === "bound_to_different_did",
);
assert.deepEqual(again, []);
assert.equal(decodeBindNonce(`0x${"00".repeat(31)}07`), 7n);
assert.equal(decodeBoundDid(`0x${"00".repeat(128)}`), null);
refuses("malformed_precompile_answer", () => decodeBindNonce("0x01"));
refuses("malformed_precompile_answer", () => decodeBoundDid(`0x${"00".repeat(64)}`));

console.log("account derivation: ok");

test("derive_and_bind", () => {
  const seed = bytes(fixture.mnemonic_vectors[0].seed);
  const pair = deriveFromSeed(seed);
  assert.equal(pair.evmAddress, expected.evm_address);
  assert.equal(pair.did, expected.did);
  const plan = planLayerXBind(derived, expected.bind.chain_id, { boundDidPublicKey: null, nonce: 0n });
  assert.equal(plan.action, "bind");
  if (plan.action !== "bind") assert.fail("bind plan required");
  assert.deepEqual(bindTransactionRequest(derived, plan.call), {
    method: "eth_sendTransaction",
    params: [{ from: derived.evmAddress, to: expected.bind.to, data: `0x${expected.bind.calldata}`, value: "0x0" }],
  });
  const answer = `0x${encodeAbiCall("binding", ["bytes32", "string"], [`0x${derived.layerxPublicKey}`, derived.did]).slice(10)}`;
  assert.equal(decodeLayerXDid(answer), derived.layerxPublicKey);
  refuses("malformed_precompile_answer", () => decodeLayerXDid(`${answer}00`));
  refuses("malformed_precompile_answer", () => decodeLayerXDid(`0x${"00".repeat(192)}`));
  refuses("malformed_precompile_answer", () => decodeLayerXDid(`${answer.slice(0, -2)}01`));
});

test("unbound_write_refused", () => {
  let signatures = 0;
  refuses("address_unbound", () => {
    requireLayerXBinding(derived, { boundDidPublicKey: null, nonce: 0n });
    signatures += 1;
    secp256k1.sign(new Uint8Array(32), derived.evmPrivateKey as Uint8Array, { prehash: false });
  });
  assert.equal(signatures, 0);
  refuses("bound_to_different_did", () =>
    requireLayerXBinding(derived, { boundDidPublicKey: "aa".repeat(32), nonce: 1n }),
  );
  assert.equal(
    requireLayerXBinding(derived, { boundDidPublicKey: derived.layerxPublicKey.toUpperCase(), nonce: 1n }),
    undefined,
  );
  refuses("malformed_precompile_answer", () => layerXBindMessage(125, derived.evmAddress, -1n));
  refuses("malformed_precompile_answer", () => layerXBindMessage(125, derived.evmAddress, 1n << 64n));
  assert.equal(hex(layerXBindMessage(125, derived.evmAddress, (1n << 64n) - 1n)).slice(-16), "ffffffffffffffff");
});

test("bind_message_vector_v6", () => {
  const rust = JSON.parse(
    readFileSync(new URL("../../../../../layerxproof/testdata/paxeer_bind_vectors.json", import.meta.url), "utf8"),
  );
  const vector = rust.binds.find((entry: { name: string }) => entry.name === "signed-for-another-chain");
  assert.equal(vector.valid, false);
  const address = `0x${vector.evm_address}`;
  const message = "4c583a5041584545522d42494e443a7631" + "00".repeat(31) + "7d" + vector.evm_address + "00".repeat(8);
  assert.equal(hex(layerXBindMessage(125, address, 0n)), message);
  const pair = {
    ...derived,
    evmAddress: address,
    layerxSeed: new Uint8Array(32).fill(0x61),
    layerxPublicKey: rust.public_key,
    did: rust.did,
  };
  const plan = planLayerXBind(pair, 125, { boundDidPublicKey: null, nonce: 0n });
  assert.equal(plan.action, "bind");
  if (plan.action !== "bind") assert.fail("bind plan required");
  assert.equal(plan.call.signature, vector.signature);
  assert.equal(
    plan.call.data,
    `0xdd9aa628${rust.public_key}${"00".repeat(31)}40${"00".repeat(31)}40${vector.signature}`,
  );
  const result = JSON.parse(
    execFileSync(
      "python3",
      [
        "-c",
        `
import json
import sys
from dataclasses import replace
from layerx_sdk.account_derivation import (
    AccountDerivationError, BindFees, LayerXBindState, bind_transaction_request,
    decode_layerx_did, derive_from_mnemonic, layerx_bind_message,
    plan_layerx_bind, require_layerx_binding, sign_bind_transaction,
)
payload = json.loads(sys.argv[1])
account = derive_from_mnemonic(payload["mnemonic"])
assert account.evm_address == payload["evm_address"]
assert account.layerx_public_key == payload["public_key"]
pair = replace(account, evm_address=payload["address"], layerx_seed=bytes([0x61]) * 32,
               layerx_public_key=payload["rust_public_key"], did=payload["rust_did"])
plan = plan_layerx_bind(pair, 125, LayerXBindState(None, 0))
assert plan.call is not None
assert plan.call.signature == payload["signature"]
assert layerx_bind_message(125, pair.evm_address, 0).hex() == payload["message"]
assert decode_layerx_did(payload["did_answer"]) == account.layerx_public_key
for state, code in ((LayerXBindState(None, 0), "address_unbound"),
                    (LayerXBindState("aa" * 32, 1), "bound_to_different_did")):
    try:
        require_layerx_binding(account, state)
    except AccountDerivationError as error:
        assert error.code == code
    else:
        raise AssertionError(code)
require_layerx_binding(account, LayerXBindState(account.layerx_public_key.upper(), 1))
for nonce in (-1, 1 << 64, True):
    try:
        layerx_bind_message(125, pair.evm_address, nonce)
    except AccountDerivationError as error:
        assert error.code == "malformed_precompile_answer"
    else:
        raise AssertionError("nonce bound")
own_plan = plan_layerx_bind(account, 125, LayerXBindState(None, 0))
assert own_plan.call is not None
fees = BindFees(0, 1, 2, 150000)
raw = sign_bind_transaction(account, 125, own_plan.call, fees)
for bad_fees in (replace(fees, evm_nonce=-1), replace(fees, gas_limit=0),
                 replace(fees, max_priority_fee_per_gas=3), replace(fees, max_fee_per_gas=1 << 128)):
    try:
        sign_bind_transaction(account, 125, own_plan.call, bad_fees)
    except AccountDerivationError as error:
        assert error.code == "invalid_bind_fees"
    else:
        raise AssertionError("fee bound")
try:
    sign_bind_transaction(account, 125, replace(own_plan.call, to=account.evm_address), fees)
except AccountDerivationError as error:
    assert error.code == "malformed_precompile_answer"
else:
    raise AssertionError("bind destination")
print(json.dumps({"message": layerx_bind_message(125, pair.evm_address, 0).hex(),
                  "data": plan.call.data, "transaction": bind_transaction_request(pair, plan.call),
                  "raw": raw}))
`,
        JSON.stringify({
          mnemonic: fixture.mnemonic_vectors[0].mnemonic,
          evm_address: derived.evmAddress,
          public_key: derived.layerxPublicKey,
          address,
          rust_public_key: rust.public_key,
          rust_did: rust.did,
          signature: vector.signature,
          message,
          did_answer: `0x${encodeAbiCall("binding", ["bytes32", "string"], [`0x${derived.layerxPublicKey}`, derived.did]).slice(10)}`,
        }),
      ],
      {
        env: {
          ...process.env,
          PYTHONPATH: fileURLToPath(new URL("../../../python", import.meta.url)),
          PYTHONDONTWRITEBYTECODE: "1",
        },
        encoding: "utf8",
      },
    ),
  );
  assert.equal(result.message, message);
  assert.equal(result.data, plan.call.data);
  assert.deepEqual(result.transaction, { from: address, to: plan.call.to, data: plan.call.data, value: "0x0" });
  const decodeList = (raw: Uint8Array): Uint8Array[] => {
    assert.equal(raw[0], 2);
    let at = 1;
    const take = (): Uint8Array => {
      const prefix = raw[at++];
      assert.notEqual(prefix, undefined);
      if (prefix === undefined) assert.fail("RLP prefix required");
      if (prefix < 0x80) return new Uint8Array([prefix]);
      if (prefix >= 0xc0) {
        assert.equal(prefix, 0xc0);
        return new Uint8Array(0);
      }
      let length = prefix - 0x80;
      if (prefix >= 0xb8) {
        const size = prefix - 0xb7;
        length = Number(BigInt(`0x${hex(raw.slice(at, at + size))}`));
        at += size;
      }
      const value = raw.slice(at, at + length);
      assert.equal(value.length, length);
      at += length;
      return value;
    };
    const listPrefix = raw[at++];
    assert.notEqual(listPrefix, undefined);
    if (listPrefix === undefined || listPrefix < 0xf8) assert.fail("long RLP list required");
    const size = listPrefix - 0xf7;
    const length = Number(BigInt(`0x${hex(raw.slice(at, at + size))}`));
    at += size;
    assert.equal(at + length, raw.length);
    const fields: Uint8Array[] = [];
    while (at < raw.length) fields.push(take());
    assert.equal(at, raw.length);
    return fields;
  };
  const ownPlan = planLayerXBind(derived, 125, { boundDidPublicKey: null, nonce: 0n });
  assert.equal(ownPlan.action, "bind");
  if (ownPlan.action !== "bind") assert.fail("bind plan required");
  const fees = { evmNonce: 0n, maxPriorityFeePerGas: 1n, maxFeePerGas: 2n, gasLimit: 150000n };
  const reference = decodeList(bytes(signBindTransaction(derived, 125, ownPlan.call, fees).slice(2)));
  const signed = decodeList(bytes(result.raw.slice(2)));
  assert.equal(signed.length, 12);
  assert.deepEqual(signed.slice(0, 9), reference.slice(0, 9));
  const integer = (value: Uint8Array | undefined): bigint => {
    assert.notEqual(value, undefined);
    if (value === undefined) assert.fail("integer required");
    return value.length === 0 ? 0n : BigInt(`0x${hex(value)}`);
  };
  const parity = integer(signed[9]);
  const r = integer(signed[10]);
  const s = integer(signed[11]);
  assert.ok(parity === 0n || parity === 1n);
  assert.ok(r > 0n && r < secp256k1.Point.Fn.ORDER);
  assert.ok(s > 0n && s <= secp256k1.Point.Fn.ORDER / 2n);
  const compact = bytes(r.toString(16).padStart(64, "0") + s.toString(16).padStart(64, "0"));
  const encode = (raw: Uint8Array): Uint8Array => {
    if (raw.length === 1 && (raw[0] ?? 0) < 0x80) return raw;
    const size = bytes(raw.length.toString(16).padStart(2, "0"));
    return new Uint8Array([...(raw.length < 56 ? [0x80 + raw.length] : [0xb7 + size.length, ...size]), ...raw]);
  };
  const payload = new Uint8Array(
    signed.slice(0, 9).flatMap((field, index) => (index === 8 ? [0xc0] : [...encode(field)])),
  );
  const length = bytes(payload.length.toString(16).padStart(2, "0"));
  const unsigned = new Uint8Array([2, 0xf7 + length.length, ...length, ...payload]);
  const publicKey = secp256k1.Signature.fromBytes(compact, "compact")
    .addRecoveryBit(Number(parity))
    .recoverPublicKey(keccak_256(unsigned))
    .toBytes(false);
  assert.equal(hex(publicKey), hex(secp256k1.getPublicKey(derived.evmPrivateKey as Uint8Array, false)));
  refuses("invalid_bind_fees", () => signBindTransaction(derived, 125, ownPlan.call, { ...fees, evmNonce: -1n }));
  refuses("malformed_precompile_answer", () =>
    signBindTransaction(derived, 125, { ...ownPlan.call, to: derived.evmAddress }, fees),
  );
});
