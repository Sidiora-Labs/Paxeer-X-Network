import { decodeCustodyAuthorization, encodeCustodyAuthorization, PAXEER_CHAIN_ID, type CustodyAuthorization, decodeCustodyStatus } from "@paxeer/wallet/provider";
import { keccak256, type Hex } from "viem";
import type { WalletSignRequest } from "../../api/index.ts";

export type WalletHandOffPhase =
  | "idle"
  | "waiting"
  | "approved"
  | "rejected"
  | "cancelled"
  | "unavailable"
  | "failed";

export type WalletSignOutcome =
  | Readonly<{ outcome: "approved"; reference: string }>
  | Readonly<{ outcome: "rejected" }>
  | Readonly<{ outcome: "cancelled" }>
  | Readonly<{ outcome: "unavailable" }>
  | Readonly<{ outcome: "failed" }>;

export interface PaxeerWalletBridge {
  sign(request: WalletSignRequest): Promise<WalletSignOutcome>;
}

export class WalletHandOff {
  readonly #bridge: PaxeerWalletBridge;
  readonly #approvedStages = new Set<string>();
  readonly #opens = new Map<string, number>();
  #phase: WalletHandOffPhase = "idle";
  #reference: string | undefined;
  #attempt = 0;

  constructor(bridge: PaxeerWalletBridge) {
    this.#bridge = bridge;
  }

  get phase(): WalletHandOffPhase {
    return this.#phase;
  }

  get reference(): string | undefined {
    return this.#reference;
  }

  opens(stageId: string): number {
    return this.#opens.get(stageId) ?? 0;
  }

  approved(stageId: string): boolean {
    return this.#approvedStages.has(stageId);
  }

  cancel(): void {
    if (this.#phase === "waiting") {
      this.#phase = "cancelled";
      this.#attempt += 1;
    }
  }

  async open(request: WalletSignRequest): Promise<WalletSignOutcome> {
    if (this.#phase === "waiting") {
      throw new Error("The wallet is already open for a signing moment");
    }
    if (this.#approvedStages.has(request.stage_id)) {
      throw new Error("This signing moment has already been approved");
    }
    this.#attempt += 1;
    const attempt = this.#attempt;
    this.#phase = "waiting";
    this.#opens.set(request.stage_id, this.opens(request.stage_id) + 1);
    let outcome: WalletSignOutcome;
    try {
      outcome = await this.#bridge.sign(request);
    } catch {
      outcome = { outcome: "failed" };
    }
    if (this.#attempt !== attempt) {
      return { outcome: "cancelled" };
    }
    this.#phase = outcome.outcome;
    if (outcome.outcome === "approved") {
      this.#approvedStages.add(request.stage_id);
      this.#reference = outcome.reference;
    }
    return outcome;
  }
}

export interface Eip1193Provider {
  request(args: Readonly<{ method: string; params?: readonly unknown[] }>): Promise<unknown>;
}

const USER_REJECTED_REQUEST = 4001;
const UNAUTHORIZED_REQUEST = 4100;
const PROVIDER_DISCONNECTED = 4900;
const CHAIN_DISCONNECTED = 4901;

export interface CustodyHandoffStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface CustodyHandoffRecord {
  readonly version: 1;
  readonly stage_id: string;
  readonly account: string;
  readonly chain_id: string;
  readonly custody: Hex;
  readonly signature: Hex | null;
  readonly phase: "signing" | "signed" | "sending" | "submitted";
  readonly tx_hash: Hex | null;
}

export interface CustodyBridgeOptions {
  readonly storage?: CustodyHandoffStorage;
  readonly expectedChainId?: bigint;
}

const SIGNATURE = /^0x[0-9a-f]{130}$/;
const TRANSACTION = /^0x[0-9a-f]{64}$/;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

export function custodyFromWalletSignRequest(
  request: WalletSignRequest,
  expectedChainId = BigInt(PAXEER_CHAIN_ID),
): Readonly<{ custody: Hex; authorization: CustodyAuthorization }> {
  const text = request.to_sign_base64;
  if (typeof text !== "string" || text.length === 0 || text.length > 2732
    || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(text)) {
    throw new Error("Canonical custody bytes are required");
  }
  const binary = atob(text);
  if (btoa(binary) !== text) throw new Error("Noncanonical custody bytes");
  const custody = ("0x" + Array.from(binary, byte => byte.charCodeAt(0).toString(16).padStart(2, "0")).join("")) as Hex;
  const authorization = decodeCustodyAuthorization(custody);
  if (encodeCustodyAuthorization(authorization) !== custody
    || !ADDRESS.test(request.from_address) || authorization.account.toLowerCase() !== request.from_address.toLowerCase()
    || expectedChainId <= 0n || authorization.chainId !== expectedChainId
    || (request.settlement_domain !== undefined && request.settlement_domain !== "paxeer")
    || typeof request.stage_id !== "string" || request.stage_id.length === 0
    || new TextEncoder().encode(request.stage_id).length > 255) {
    throw new Error("Custody account or network does not match the request");
  }
  return Object.freeze({ custody, authorization });
}

export function custodyHandoffStorageKey(request: WalletSignRequest): string {
  return `layerx:private-custody:v2:${request.from_address.toLowerCase()}:${request.stage_id}`;
}

function retainedRecord(text: string, request: WalletSignRequest, custody: Hex, chain: bigint): CustodyHandoffRecord {
  if (text.length > 16_384) throw new Error("Invalid retained custody record");
  const value: unknown = JSON.parse(text);
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("Invalid retained custody record");
  const record = value as Record<string, unknown>;
  const fields = ["version", "stage_id", "account", "chain_id", "custody", "signature", "phase", "tx_hash"];
  if (Object.keys(record).length !== fields.length || fields.some(field => !(field in record))
    || record.version !== 1 || record.stage_id !== request.stage_id
    || record.account !== request.from_address.toLowerCase() || record.chain_id !== chain.toString()
    || record.custody !== custody
    || (record.signature !== null && (typeof record.signature !== "string" || !SIGNATURE.test(record.signature)))
    || !["signing", "signed", "sending", "submitted"].includes(String(record.phase))
    || (record.tx_hash !== null && (typeof record.tx_hash !== "string" || !TRANSACTION.test(record.tx_hash) || /^0x0{64}$/.test(record.tx_hash)))
    || (record.phase !== "signing" && record.signature === null)
    || (record.phase === "signing" && record.signature !== null)
    || (record.phase === "submitted" ? record.tx_hash === null : record.tx_hash !== null)) {
    throw new Error("Retained custody identity or bytes changed");
  }
  return record as unknown as CustodyHandoffRecord;
}

async function requireCustodyWallet(wallet: Eip1193Provider, request: WalletSignRequest, chain: bigint): Promise<void> {
  const accounts = await wallet.request({ method: "eth_accounts" });
  const network = await wallet.request({ method: "eth_chainId" });
  if (!Array.isArray(accounts) || typeof accounts[0] !== "string" || !ADDRESS.test(accounts[0])
    || accounts[0].toLowerCase() !== request.from_address.toLowerCase()
    || typeof network !== "string" || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/.test(network) || BigInt(network) !== chain) {
    throw Object.assign(new Error("The connected custody account or chain differs"), { code: UNAUTHORIZED_REQUEST });
  }
}

export function browserWalletBridge(
  provider: () => Eip1193Provider | undefined,
  options: CustodyBridgeOptions = {},
): PaxeerWalletBridge {
  return {
    async sign(request: WalletSignRequest): Promise<WalletSignOutcome> {
      let decoded: ReturnType<typeof custodyFromWalletSignRequest>;
      try { decoded = custodyFromWalletSignRequest(request, options.expectedChainId); }
      catch { return { outcome: "rejected" }; }
      const wallet = provider();
      if (wallet === undefined) return { outcome: "unavailable" };
      let storage: CustodyHandoffStorage | undefined;
      let record: CustodyHandoffRecord | undefined;
      const key = custodyHandoffStorageKey(request);
      const { custody, authorization } = decoded;
      try {
        storage = options.storage ?? (typeof window === "undefined" ? undefined : window.sessionStorage);
        if (storage === undefined) throw new Error("Durable custody storage is unavailable");
        await requireCustodyWallet(wallet, request, authorization.chainId);
        const saved = storage.getItem(key);
        if (saved !== null) record = retainedRecord(saved, request, custody, authorization.chainId);
        if (record?.phase === "signing" || record?.phase === "sending" || record?.phase === "submitted") {
          const status = decodeCustodyStatus(await wallet.request({ method: "paxeer_custodyStatus", params: [{ custody }] }), keccak256(custody));
          if (typeof status !== "object" || status === null || !["pending", "confirmed", "reverted"].includes(status.status)) {
            throw new Error("Custody status is unavailable");
          }
          if (status.status === "reverted") return { outcome: "rejected" };
          if (typeof status.tx_hash !== "string" || !TRANSACTION.test(status.tx_hash) || /^0x0{64}$/.test(status.tx_hash)) {
            return { outcome: "failed" };
          }
          if (record.tx_hash !== null && record.tx_hash !== status.tx_hash) throw new Error("Custody transaction changed");
          if (record.signature === null) return { outcome: "failed" };
          await wallet.request({ method: "paxeer_restoreCustody", params: [{ custody, signature: record.signature }] });
          await requireCustodyWallet(wallet, request, authorization.chainId);
          record = { ...record, phase: "submitted", tx_hash: status.tx_hash };
          storage.setItem(key, JSON.stringify(record));
          return { outcome: "approved", reference: status.tx_hash };
        }
        if (record === undefined) {
          record = { version: 1, stage_id: request.stage_id, account: request.from_address.toLowerCase(),
            chain_id: authorization.chainId.toString(), custody, signature: null, phase: "signing", tx_hash: null };
          storage.setItem(key, JSON.stringify(record));
          const signature = await wallet.request({ method: "paxeer_signCustody", params: [{ custody }] });
          if (typeof signature !== "string" || !SIGNATURE.test(signature.toLowerCase())) throw new Error("Invalid custody signature");
          record = { ...record, signature: signature.toLowerCase() as Hex, phase: "signed" };
          storage.setItem(key, JSON.stringify(record));
        }
        if (record.signature === null) return { outcome: "failed" };
        await wallet.request({ method: "paxeer_restoreCustody", params: [{ custody, signature: record.signature }] });
        await requireCustodyWallet(wallet, request, authorization.chainId);
        record = { ...record, phase: "sending" };
        storage.setItem(key, JSON.stringify(record));
        const reference = await wallet.request({ method: "eth_sendTransaction", params: [{
          from: authorization.account, chainId: "0x" + authorization.chainId.toString(16),
          to: authorization.to, value: "0x" + authorization.value.toString(16), data: authorization.data,
          nonce: "0x" + authorization.nonce.toString(16), gas: "0x" + authorization.gas.toString(16),
          maxFeePerGas: "0x" + authorization.maxFeePerGas.toString(16),
          maxPriorityFeePerGas: "0x" + authorization.maxPriorityFeePerGas.toString(16),
        }] });
        if (typeof reference !== "string" || !TRANSACTION.test(reference.toLowerCase()) || /^0x0{64}$/i.test(reference)) throw new Error("Invalid custody transaction reference");
        await requireCustodyWallet(wallet, request, authorization.chainId);
        record = { ...record, phase: "submitted", tx_hash: reference.toLowerCase() as Hex };
        storage.setItem(key, JSON.stringify(record));
        return { outcome: "approved", reference: record.tx_hash! };
      } catch (error) {
        const code = (error as Readonly<{ code?: unknown }>).code;
        if (code === USER_REJECTED_REQUEST) {
          try {
            if (storage !== undefined && record?.phase === "signing") storage.removeItem(key);
            else if (storage !== undefined && record?.phase === "sending") storage.setItem(key, JSON.stringify({ ...record, phase: "signed" }));
          } catch { return { outcome: "failed" }; }
          return { outcome: "cancelled" };
        }
        if (code === UNAUTHORIZED_REQUEST && (record?.phase === "signing" || record?.phase === "sending" || record?.phase === "submitted")) {
          return { outcome: "failed" };
        }
        if (code === UNAUTHORIZED_REQUEST) return { outcome: "rejected" };
        return code === PROVIDER_DISCONNECTED || code === CHAIN_DISCONNECTED
          ? { outcome: "unavailable" } : { outcome: "failed" };
      }
    },
  };
}

export function windowWalletProvider(): Eip1193Provider | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  const candidate = (window as Readonly<{ paxeer?: unknown }>).paxeer;
  if (typeof candidate !== "object" || candidate === null) {
    return undefined;
  }
  const request = (candidate as Readonly<{ request?: unknown }>).request;
  return typeof request === "function" ? (candidate as Eip1193Provider) : undefined;
}
