/**
 * Client for the bridge backend your devs run (see BACKEND.md in this app).
 *
 * The backend is an attestation courier, nothing more: attestors watch the
 * vaults and the precompile and the backend serves the threshold signature
 * bundles that credit a deposit (bridgeIn) or pay out a burn (release).
 * Nothing it returns is trusted — every payload is re-validated here and
 * its signatures are verified against the on-chain attestor set before the
 * wallet is asked to submit anything.
 */
import { getAddress, isAddress, isHash, isHex, type Address, type Hex } from "viem";

import { bridgeConfig } from "./config";

export type AttestationStatus = "observed" | "finalizing" | "attesting" | "ready" | "unknown";

export type DepositAttestation = {
  chainId: string;
  vault: Address;
  txHash: Hex;
  logIndex: string;
  recipient32: Hex;
  asset: Address;
  amount: string;
  digest: Hex;
  signatures: Hex[];
  status: AttestationStatus;
};

export type ReleaseAttestation = {
  chainId: string;
  vault: Address;
  paxeerTxHash: Hex;
  paxeerNonce: string;
  recipient: Address;
  asset: Address;
  amount: string;
  digest: Hex;
  signatures: Hex[];
  status: AttestationStatus;
};

export class AttestationApiError extends Error {}

async function fetchJson(path: string): Promise<unknown> {
  const base = bridgeConfig.attestationApiUrl;
  if (!base) throw new AttestationApiError("The bridge backend is not configured.");
  let response: Response;
  try {
    response = await fetch(`${base}${path}`, {
      headers: { accept: "application/json" },
      cache: "no-store",
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    throw new AttestationApiError("The bridge backend could not be reached.");
  }
  if (response.status === 404) return null;
  if (!response.ok) {
    throw new AttestationApiError(`The bridge backend answered ${response.status}.`);
  }
  try {
    return (await response.json()) as unknown;
  } catch {
    throw new AttestationApiError("The bridge backend returned invalid data.");
  }
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null) {
    throw new AttestationApiError("The bridge backend returned invalid data.");
  }
  return value as Record<string, unknown>;
}

function readString(obj: Record<string, unknown>, key: string): string {
  const value = obj[key];
  if (typeof value !== "string") {
    throw new AttestationApiError(`The bridge backend is missing "${key}".`);
  }
  return value;
}

function readDecimal(value: string, key: string): string {
  if (!/^\d+$/.test(value)) {
    throw new AttestationApiError(`The bridge backend returned a bad "${key}".`);
  }
  return value;
}

function readAddress(value: string, key: string): Address {
  if (!isAddress(value, { strict: false })) {
    throw new AttestationApiError(`The bridge backend returned a bad "${key}".`);
  }
  return getAddress(value);
}

function readHash(value: string, key: string): Hex {
  if (!isHash(value)) {
    throw new AttestationApiError(`The bridge backend returned a bad "${key}".`);
  }
  return value as Hex;
}

function readBytes32(value: string, key: string): Hex {
  if (!isHex(value) || value.length !== 66) {
    throw new AttestationApiError(`The bridge backend returned a bad "${key}".`);
  }
  return value as Hex;
}

function readStatus(value: string): AttestationStatus {
  return value === "observed" || value === "finalizing" || value === "attesting" || value === "ready"
    ? value
    : "unknown";
}

function readSignatures(value: unknown): Hex[] {
  if (value === undefined) return [];
  if (!Array.isArray(value)) {
    throw new AttestationApiError("The bridge backend returned bad signatures.");
  }
  return value.map((entry) => {
    if (typeof entry !== "string" || !/^0x[0-9a-fA-F]{130}$/.test(entry)) {
      throw new AttestationApiError("The bridge backend returned a bad signature.");
    }
    return entry as Hex;
  });
}

/** GET /v1/deposit/{chainId}/{txHash}/{logIndex} — null while unobserved. */
export async function fetchDepositAttestation(args: {
  chainId: bigint;
  txHash: Hex;
  logIndex: bigint;
}): Promise<DepositAttestation | null> {
  const raw = await fetchJson(
    `/v1/deposit/${args.chainId}/${args.txHash.toLowerCase()}/${args.logIndex}`,
  );
  if (raw === null) return null;
  const obj = record(raw);
  const chainId = readDecimal(readString(obj, "chainId"), "chainId");
  return {
    chainId,
    vault: readAddress(readString(obj, "vault"), "vault"),
    txHash: readHash(readString(obj, "txHash"), "txHash"),
    logIndex: readDecimal(readString(obj, "logIndex"), "logIndex"),
    recipient32: readBytes32(readString(obj, "recipient"), "recipient"),
    asset: readAddress(readString(obj, "asset"), "asset"),
    amount: readDecimal(readString(obj, "amount"), "amount"),
    digest: readBytes32(readString(obj, "digest"), "digest"),
    signatures: readSignatures(obj["signatures"]),
    status: readStatus(readString(obj, "status")),
  };
}

/** GET /v1/release/{chainId}/{paxeerTxHash}/{nonce} — null while unobserved. */
export async function fetchReleaseAttestation(args: {
  chainId: bigint;
  paxeerTxHash: Hex;
  paxeerNonce: bigint;
}): Promise<ReleaseAttestation | null> {
  const raw = await fetchJson(
    `/v1/release/${args.chainId}/${args.paxeerTxHash.toLowerCase()}/${args.paxeerNonce}`,
  );
  if (raw === null) return null;
  const obj = record(raw);
  return {
    chainId: readDecimal(readString(obj, "chainId"), "chainId"),
    vault: readAddress(readString(obj, "vault"), "vault"),
    paxeerTxHash: readHash(readString(obj, "paxeerTxHash"), "paxeerTxHash"),
    paxeerNonce: readDecimal(readString(obj, "paxeerNonce"), "paxeerNonce"),
    recipient: readAddress(readString(obj, "recipient"), "recipient"),
    asset: readAddress(readString(obj, "asset"), "asset"),
    amount: readDecimal(readString(obj, "amount"), "amount"),
    digest: readBytes32(readString(obj, "digest"), "digest"),
    signatures: readSignatures(obj["signatures"]),
    status: readStatus(readString(obj, "status")),
  };
}
