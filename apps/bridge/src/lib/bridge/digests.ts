/**
 * The attestation digests attestors sign — byte-exact per
 * contracts/bridge/evm/src/BridgeAttestation.sol and
 * modules/layerxbridge/ATTESTATION.md. Raw keccak256 over packed big-endian
 * fields; no EIP-191 prefix, no EIP-712 structure.
 *
 * The frontend computes these only to *verify* what the backend returns:
 *   - ecrecover each signature over the digest and check the signer set
 *     against getAttestors()/attestors() before asking the wallet to submit
 *     bridgeIn/release. The chain enforces the same rules; this keeps a
 *     wrong or hostile backend from wasting the user's gas or misreporting.
 * Signatures bind no funds beyond the attested event itself — the preimage
 * names vault, recipient, asset and amount.
 */
import {
  concat,
  encodePacked,
  getAddress,
  keccak256,
  numberToHex,
  pad,
  recoverAddress,
  stringToHex,
  type Address,
  type Hex,
} from "viem";

/** 20-byte domain, inbound (BridgeAttestation.DOMAIN_IN). */
export const DOMAIN_IN = "PAXEERX_BRIDGE_IN_V1";
/** 21-byte domain, outbound (BridgeAttestation.DOMAIN_OUT). */
export const DOMAIN_OUT = "PAXEERX_BRIDGE_OUT_V1";

const SECP256K1_HALF_ORDER = 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0n;

/** The canonical 32-byte Paxeer recipient: EVM address in the low 20 bytes. */
export function paxeerRecipient(address: Address): Hex {
  return pad(getAddress(address), { size: 32 });
}

/**
 * bridgeIn preimage: bytes20 domain || uint256 chainId || address vault ||
 * bytes32 txHash || uint64 logIndex || bytes32 recipient || address asset ||
 * uint256 amount  (196 bytes).
 */
export function inboundPreimage(args: {
  chainId: bigint;
  vault: Address;
  txHash: Hex;
  logIndex: bigint;
  recipient32: Hex;
  asset: Address;
  amount: bigint;
}): Hex {
  return encodePacked(
    ["bytes", "uint256", "address", "bytes32", "uint64", "bytes32", "address", "uint256"],
    [
      stringToHex(DOMAIN_IN),
      args.chainId,
      getAddress(args.vault),
      args.txHash,
      args.logIndex,
      args.recipient32,
      getAddress(args.asset),
      args.amount,
    ],
  );
}

export function inboundDigest(args: Parameters<typeof inboundPreimage>[0]): Hex {
  return keccak256(inboundPreimage(args));
}

/**
 * Vault release preimage: bytes21 domain || uint256 chainId || address vault
 * || bytes32 paxeerTxHash || uint64 paxeerNonce || address recipient ||
 * address asset || uint256 amount  (185 bytes).
 * For Solana releases the recipient slot carries keccak256(pubkey)[12:32] —
 * see solanaRecipientHandle.
 */
export function outboundPreimage(args: {
  chainId: bigint;
  vault: Address;
  paxeerTxHash: Hex;
  paxeerNonce: bigint;
  recipient: Address;
  asset: Address;
  amount: bigint;
}): Hex {
  return encodePacked(
    ["bytes", "uint256", "address", "bytes32", "uint64", "address", "address", "uint256"],
    [
      stringToHex(DOMAIN_OUT),
      args.chainId,
      getAddress(args.vault),
      args.paxeerTxHash,
      args.paxeerNonce,
      getAddress(args.recipient),
      getAddress(args.asset),
      args.amount,
    ],
  );
}

export function outboundDigest(args: Parameters<typeof outboundPreimage>[0]): Hex {
  return keccak256(outboundPreimage(args));
}

/** Vault release nullifier: keccak256(paxeerTxHash || uint64 paxeerNonce). */
export function releaseNullifier(paxeerTxHash: Hex, paxeerNonce: bigint): Hex {
  return keccak256(concat([paxeerTxHash, numberToHex(paxeerNonce, { size: 8 })]));
}

export type SignatureCheck =
  | { ok: true; signers: Address[] }
  | { ok: false; reason: string };

/**
 * Verify a threshold signature bundle exactly as the contracts do: each is
 * 65 bytes r||s||v with v ∈ {27, 28}, s in the low half, signers strictly
 * ascending, every signer in the attestor set, at least threshold of them.
 */
export async function verifyThresholdSignatures(args: {
  digest: Hex;
  signatures: readonly Hex[];
  attestors: readonly Address[];
  threshold: number;
}): Promise<SignatureCheck> {
  if (args.signatures.length < args.threshold) {
    return { ok: false, reason: `Only ${args.signatures.length} of ${args.threshold} required signatures are available.` };
  }
  const allowed = new Set(args.attestors.map((a) => getAddress(a).toLowerCase()));
  const signers: Address[] = [];
  for (const [index, signature] of args.signatures.entries()) {
    if (!/^0x[0-9a-fA-F]{130}$/.test(signature)) {
      return { ok: false, reason: `Signature ${index + 1} is not 65 bytes.` };
    }
    const v = Number.parseInt(signature.slice(130, 132), 16);
    if (v !== 27 && v !== 28) {
      return { ok: false, reason: `Signature ${index + 1} has an invalid recovery id.` };
    }
    const s = BigInt(`0x${signature.slice(66, 130)}`);
    if (s > SECP256K1_HALF_ORDER) {
      return { ok: false, reason: `Signature ${index + 1} is not low-s.` };
    }
    let signer: Address;
    try {
      signer = await recoverAddress({ hash: args.digest, signature });
    } catch {
      return { ok: false, reason: `Signature ${index + 1} does not recover a signer.` };
    }
    if (!allowed.has(signer.toLowerCase())) {
      return { ok: false, reason: `Signature ${index + 1} is not from a registered attestor.` };
    }
    const previous = signers[signers.length - 1];
    if (previous && BigInt(signer) <= BigInt(previous)) {
      return { ok: false, reason: "Signatures are not ordered by ascending signer." };
    }
    signers.push(signer);
  }
  return { ok: true, signers };
}
