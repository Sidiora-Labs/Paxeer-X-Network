import { secp256k1 } from "@noble/curves/secp256k1.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import { abiEventTopic, abiSelector, encodeAbiCall, type PrecompileCall } from "./exchange.js";

export const SIDIORA_TOKEN = "0x21f7b20a555199fa73A238B1a91FD0f549068fEe";
export const SIDIORA_DECIMALS = 6;
export const GAS_STATION_QUOTE_URL = "https://api-mainnet-beta.paxeer.network/gas-station/quote";

export interface GasQuote {
  readonly sponsor: string;
  readonly token: string;
  readonly maxTokenAmount: bigint;
  readonly tokenAmount: bigint;
  readonly deadline: bigint;
  readonly quoteNonce: bigint;
  readonly gasCost: bigint;
  readonly decimals: number;
}

export interface GasStationConfig {
  readonly quoteUrl?: string;
  readonly chainId: bigint;
  readonly sponsor: string;
  readonly token: string;
  readonly decimals: number;
  readonly paymaster: string;
}

export interface SponsoredBatch {
  readonly chainId: bigint;
  readonly account: string;
  readonly nonce: bigint;
  readonly calls: readonly PrecompileCall[];
  readonly quote: GasQuote;
}

export interface SignedGasQuote {
  readonly quote: GasQuote;
  readonly relayerSignature: string;
}

export interface GasQuoteRequest {
  readonly account: string;
  readonly nonce: bigint;
  readonly calls: readonly PrecompileCall[];
  readonly maxTokenAmount: bigint;
  readonly gasCost: bigint;
}

export type GasRefusalCode =
  | "incompatible_delegation"
  | "replay_state_unavailable"
  | "expired_quote"
  | "above_maximum"
  | "sponsor_mismatch"
  | "token_mismatch"
  | "decimals_mismatch"
  | "chain_mismatch"
  | "invalid_value"
  | "invalid_signature"
  | "invalid_response"
  | "unavailable"
  | "refused"
  | "cancelled";

export interface GasRefusal {
  readonly code: GasRefusalCode;
  readonly field: string;
}

export type GasResult<T> =
  | { readonly ok: true; readonly value: T }
  | { readonly ok: false; readonly refusal: GasRefusal };

export interface Eip7702Authorization {
  readonly chainId: bigint;
  readonly address: string;
  readonly nonce: bigint;
}

export interface SignedEip7702Authorization extends Eip7702Authorization {
  readonly yParity: number;
  readonly r: string;
  readonly s: string;
}

class Refusal extends Error {
  constructor(readonly code: GasRefusalCode, readonly field: string) {
    super(code);
  }
}

export function gasStationQuoteUrl(quoteUrl?: string, gatewayUrl?: string): string {
  let base: URL | undefined;
  let url: URL;
  try {
    if (gatewayUrl !== undefined) base = new URL(gatewayUrl);
    url = new URL(quoteUrl ?? (base ? "/gas-station/quote" : GAS_STATION_QUOTE_URL), base);
  } catch { throw new Refusal("invalid_value", "quoteUrl"); }
  if (!["https:", "http:"].includes(url.protocol) || url.username || url.password || url.hash || url.search
      || !url.pathname.endsWith("/quote") || (base && (base.username || base.password || base.hash || base.search
      || !["https:", "http:"].includes(base.protocol) || url.origin !== base.origin))) {
    throw new Refusal("invalid_value", "quoteUrl");
  }
  return url.toString();
}

function result<T>(build: () => T): GasResult<T> {
  try {
    return { ok: true, value: build() };
  } catch (error) {
    if (error instanceof Refusal) return { ok: false, refusal: { code: error.code, field: error.field } };
    throw error;
  }
}

function uint(value: bigint, field: string, bits = 256): bigint {
  if (typeof value !== "bigint" || value < 0n || value >= 1n << BigInt(bits)) {
    throw new Refusal("invalid_value", field);
  }
  return value;
}

function address(value: string, field: string): string {
  if (typeof value !== "string" || !/^0x[0-9a-fA-F]{40}$/u.test(value)) {
    throw new Refusal("invalid_value", field);
  }
  return value.toLowerCase();
}

function bytes(value: string, field: string): string {
  if (typeof value !== "string" || !/^0x(?:[0-9a-fA-F]{2})*$/u.test(value)) {
    throw new Refusal("invalid_value", field);
  }
  return value.slice(2).toLowerCase();
}

function unhex(body: string): Uint8Array {
  return Uint8Array.from(body.match(/../gu) ?? [], (byte) => Number.parseInt(byte, 16));
}

function hex(data: Uint8Array): string {
  return Array.from(data, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function hash(body: string): string {
  return `0x${hex(keccak_256(unhex(body)))}`;
}

function word(value: bigint): string {
  return uint(value, "word").toString(16).padStart(64, "0");
}

function dynamicBytes(body: string): string {
  return word(BigInt(body.length / 2)) + body.padEnd(Math.ceil(body.length / 64) * 64, "0");
}

function callsBody(calls: readonly PrecompileCall[]): string {
  const tuples = calls.map((call) =>
    address(call.to, "calls.to").slice(2).padStart(64, "0") +
    word(uint(call.value, "calls.value")) + word(96n) + dynamicBytes(bytes(call.data, "calls.data")),
  );
  let offset = tuples.length * 32;
  const offsets = tuples.map((tuple) => {
    const head = word(BigInt(offset));
    offset += tuple.length / 2;
    return head;
  });
  return word(BigInt(calls.length)) + offsets.join("") + tuples.join("");
}

function quoteBody(quote: GasQuote): string {
  return encodeAbiCall(
    "quote",
    ["address", "address", "uint256", "uint256", "uint256", "uint256", "uint256"],
    [
      address(quote.sponsor, "sponsor"), address(quote.token, "token"),
      uint(quote.maxTokenAmount, "maxTokenAmount"), uint(quote.tokenAmount, "tokenAmount"),
      uint(quote.deadline, "deadline"), uint(quote.quoteNonce, "quoteNonce"), uint(quote.gasCost, "gasCost"),
    ],
  ).slice(10);
}

function signedMessageHash(body: string): string {
  return hash(hex(new TextEncoder().encode("\x19Ethereum Signed Message:\n32")) + hash(body).slice(2));
}

function quoteHash(chainId: bigint, account: string, quote: GasQuote): string {
  return signedMessageHash(
    abiEventTopic("Quote(uint256 chainId,address account,address sponsor,address token,uint256 maxTokenAmount,uint256 tokenAmount,uint256 deadline,uint256 quoteNonce,uint256 gasCost)").slice(2) +
    word(uint(chainId, "chainId")) + address(account, "account").slice(2).padStart(64, "0") + quoteBody(quote),
  );
}

export function gasQuoteDigest(chainId: bigint, account: string, quote: GasQuote): GasResult<string> {
  return result(() => quoteHash(chainId, account, quote));
}

function batchHash(batch: SponsoredBatch): string {
  return signedMessageHash(
    abiEventTopic("SponsoredBatch(uint256 nonce,bytes32 callsHash,bytes32 quoteDigest)").slice(2) +
    word(uint(batch.nonce, "nonce")) + hash(word(32n) + callsBody(batch.calls)).slice(2) +
    quoteHash(batch.chainId, batch.account, batch.quote).slice(2),
  );
}

export function sponsoredBatchDigest(batch: SponsoredBatch): GasResult<string> {
  return result(() => batchHash(batch));
}

function validateConfig(config: GasStationConfig): void {
  if (uint(config.chainId, "chainId") === 0n) throw new Refusal("invalid_value", "chainId");
  if (BigInt(address(config.sponsor, "sponsor")) === 0n) throw new Refusal("invalid_value", "sponsor");
  if (BigInt(address(config.paymaster, "paymaster")) === 0n) throw new Refusal("invalid_value", "paymaster");
  if (address(config.token, "token") !== SIDIORA_TOKEN.toLowerCase()) throw new Refusal("token_mismatch", "token");
  if (config.decimals !== SIDIORA_DECIMALS) throw new Refusal("decimals_mismatch", "decimals");
}

function validateQuote(config: GasStationConfig, quote: GasQuote, now: bigint): void {
  validateConfig(config);
  quoteBody(quote);
  if (quote.decimals !== SIDIORA_DECIMALS) throw new Refusal("decimals_mismatch", "decimals");
  if (address(quote.sponsor, "sponsor") !== config.sponsor.toLowerCase()) throw new Refusal("sponsor_mismatch", "sponsor");
  if (address(quote.token, "token") !== config.token.toLowerCase()) throw new Refusal("token_mismatch", "token");
  if (uint(now, "now") > quote.deadline) throw new Refusal("expired_quote", "deadline");
  if (quote.tokenAmount > quote.maxTokenAmount) throw new Refusal("above_maximum", "maxTokenAmount");
  if (quote.tokenAmount === 0n) throw new Refusal("invalid_value", "tokenAmount");
  if (quote.gasCost === 0n) throw new Refusal("invalid_value", "gasCost");
}

function verifySignature(signature: string, digest: string, signer: string): string {
  const body = bytes(signature, "signature");
  if (body.length !== 130 || !["1b", "1c"].includes(body.slice(128))) {
    throw new Refusal("invalid_signature", "signature");
  }
  try {
    const parsed = secp256k1.Signature.fromBytes(unhex(body.slice(0, 128)), "compact");
    if (parsed.hasHighS()) throw new Refusal("invalid_signature", "signature");
    const publicKey = parsed.addRecoveryBit(Number.parseInt(body.slice(128), 16) - 27)
      .recoverPublicKey(unhex(digest.slice(2))).toBytes(false);
    if (hash(hex(publicKey.slice(1))).slice(-40) !== address(signer, "signer").slice(2)) {
      throw new Refusal("invalid_signature", "signature");
    }
  } catch {
    throw new Refusal("invalid_signature", "signature");
  }
  return body;
}

export function sponsoredBatchCall(
  config: GasStationConfig,
  batch: SponsoredBatch,
  accountSignature: string,
  relayerSignature: string,
  now: bigint = BigInt(Math.floor(Date.now() / 1000)),
): GasResult<PrecompileCall> {
  return result(() => {
    validateQuote(config, batch.quote, now);
    if (batch.chainId !== config.chainId) throw new Refusal("chain_mismatch", "chainId");
    if (address(batch.account, "account") === address(batch.quote.sponsor, "sponsor")) {
      throw new Refusal("invalid_value", "sponsor");
    }
    const calls = callsBody(batch.calls);
    const account = dynamicBytes(verifySignature(accountSignature, batchHash(batch), batch.account));
    const relayer = dynamicBytes(verifySignature(relayerSignature, quoteHash(batch.chainId, batch.account, batch.quote), batch.quote.sponsor));
    const headSize = 320n;
    const data = abiSelector("executeSponsored((address,uint256,bytes)[],(address,address,uint256,uint256,uint256,uint256,uint256),bytes,bytes)") +
      word(headSize) + quoteBody(batch.quote) + word(headSize + BigInt(calls.length / 2)) +
      word(headSize + BigInt((calls.length + account.length) / 2)) + calls + account + relayer;
    return { to: batch.account, value: 0n, data };
  });
}

function rlpBytes(body: string): string {
  const length = body.length / 2;
  if (length === 1 && Number.parseInt(body, 16) < 128) return body;
  if (length <= 55) return (128 + length).toString(16) + body;
  const size = integerHex(BigInt(length));
  return (183 + size.length / 2).toString(16) + size + body;
}

function integerHex(value: bigint): string {
  if (value === 0n) return "";
  const body = value.toString(16);
  return body.padStart(Math.ceil(body.length / 2) * 2, "0");
}

function authorizationHash(authorization: Eip7702Authorization): string {
  const nonce = uint(authorization.nonce, "authorization.nonce", 64);
  if (nonce === (1n << 64n) - 1n) throw new Refusal("invalid_value", "authorization.nonce");
  const body = rlpBytes(integerHex(uint(authorization.chainId, "authorization.chainId"))) +
    rlpBytes(address(authorization.address, "authorization.address").slice(2)) + rlpBytes(integerHex(nonce));
  const length = body.length / 2;
  const size = integerHex(BigInt(length));
  const list = length <= 55 ? (192 + length).toString(16) : (247 + size.length / 2).toString(16) + size;
  return hash("05" + list + body);
}

export function eip7702AuthorizationDigest(authorization: Eip7702Authorization): GasResult<string> {
  return result(() => authorizationHash(authorization));
}

export function assembleEip7702Authorization(
  config: GasStationConfig,
  account: string,
  nonce: bigint,
  signature: string,
): GasResult<SignedEip7702Authorization> {
  return result(() => {
    validateConfig(config);
    const authorization = { chainId: config.chainId, address: config.paymaster, nonce };
    const body = verifySignature(signature, authorizationHash(authorization), account);
    return { ...authorization, yParity: Number.parseInt(body.slice(128), 16) - 27, r: `0x${body.slice(0, 64)}`, s: `0x${body.slice(64, 128)}` };
  });
}

function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new Refusal("invalid_response", "quote");
  return value as Record<string, unknown>;
}

function wireString(value: unknown, field: string): string {
  if (typeof value !== "string") throw new Refusal("invalid_response", field);
  return value;
}

function wireUint(value: unknown, field: string): bigint {
  const text = wireString(value, field);
  if (!/^(0|[1-9][0-9]*)$/u.test(text) || text.length > 78) throw new Refusal("invalid_response", field);
  return uint(BigInt(text), field);
}


async function stationRefusal(response: Response, field: string): Promise<GasResult<never>> {
  let code: GasRefusalCode = response.status >= 500 ? "unavailable" : "refused";
  if (response.status === 422 && response.body !== null) {
    const reader = response.body.getReader(); let size = 0; const chunks: Uint8Array[] = [];
    try {
      for (;;) { const next = await reader.read(); if (next.done) break;
        size += next.value.length; if (size > 1_024) return { ok: false, refusal: { code: "invalid_response", field } };
        chunks.push(next.value); }
      const bytes = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
      const body = record(JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)));
      if (body.error === "incompatible_delegation" || body.error === "replay_state_unavailable" || body.error === "expired_quote") code = body.error;
    } catch { return { ok: false, refusal: { code: "invalid_response", field } }; }
    finally { await reader.cancel(); }
  }
  return { ok: false, refusal: { code, field } };
}

export async function requestGasQuote(
  config: GasStationConfig,
  request: GasQuoteRequest,
  options: { readonly signal?: AbortSignal; readonly now?: bigint; readonly fetch?: typeof fetch } = {},
): Promise<GasResult<SignedGasQuote>> {
  const prepared = result(() => {
    validateConfig(config);
    address(request.account, "account");
    uint(request.nonce, "nonce");
    callsBody(request.calls);
    if (uint(request.maxTokenAmount, "maxTokenAmount") === 0n || uint(request.gasCost, "gasCost") === 0n) {
      throw new Refusal("invalid_value", "request");
    }
    let url: URL;
    try { url = new URL(gasStationQuoteUrl(config.quoteUrl)); } catch { throw new Refusal("invalid_value", "quoteUrl"); }
    if (!["https:", "http:"].includes(url.protocol) || url.username || url.password || url.hash) {
      throw new Refusal("invalid_value", "quoteUrl");
    }
    return JSON.stringify({ ...request, chainId: config.chainId, token: config.token, decimals: config.decimals },
      (_key, value: unknown) => typeof value === "bigint" ? value.toString() : value);
  });
  if (!prepared.ok) return prepared;
  let response: Response;
  try {
    response = await (options.fetch ?? fetch)(gasStationQuoteUrl(config.quoteUrl), {
      method: "POST", headers: { "content-type": "application/json" }, body: prepared.value,
      signal: options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000),
      redirect: "error", credentials: "omit", mode: "cors",
    });
  } catch {
    return { ok: false, refusal: { code: options.signal?.aborted ? "cancelled" : "unavailable", field: "quoteUrl" } };
  }
  if (!response.ok) return stationRefusal(response, "quoteUrl");
  let payload: unknown;
  try { payload = await response.json(); } catch { return { ok: false, refusal: { code: "invalid_response", field: "quote" } }; }
  return result(() => {
    const envelope = record(payload);
    const data = record(envelope.quote);
    if (typeof data.decimals !== "number") throw new Refusal("invalid_response", "decimals");
    const quote: GasQuote = {
      sponsor: wireString(data.sponsor, "sponsor"), token: wireString(data.token, "token"), decimals: data.decimals,
      maxTokenAmount: wireUint(data.maxTokenAmount, "maxTokenAmount"), tokenAmount: wireUint(data.tokenAmount, "tokenAmount"),
      deadline: wireUint(data.deadline, "deadline"), quoteNonce: wireUint(data.quoteNonce, "quoteNonce"), gasCost: wireUint(data.gasCost, "gasCost"),
    };
    validateQuote(config, quote, options.now ?? BigInt(Math.floor(Date.now() / 1000)));
    if (quote.maxTokenAmount > request.maxTokenAmount) throw new Refusal("above_maximum", "maxTokenAmount");
    if (quote.gasCost !== request.gasCost) throw new Refusal("invalid_response", "gasCost");
    const relayerSignature = wireString(envelope.relayerSignature, "relayerSignature");
    verifySignature(relayerSignature, quoteHash(config.chainId, request.account, quote), quote.sponsor);
    return { quote, relayerSignature };
  });
}

export interface GasSubmissionIdentity {
  readonly sponsor: string;
  readonly quoteNonce: bigint;
  readonly account: string;
  readonly relayerSignature: string;
}

export interface GasSubmissionTransaction {
  readonly sponsorNonce: bigint;
  readonly transactionHash: string;
}

export type GasSubmissionOutcome =
  | { readonly outcome: "consumed" }
  | { readonly outcome: "included"; readonly transactionHash: string; readonly blockNumber: bigint; readonly sidCollected: bigint; readonly paxSpent: bigint }
  | { readonly outcome: "reverted"; readonly transactionHash: string }
  | { readonly outcome: "cancelled"; readonly transactionHash: string; readonly blockNumber: bigint };

export interface GasSubmissionStatus {
  readonly state: "quoted" | "pending" | "replacing" | "completed";
  readonly deadline: bigint;
  readonly submission: GasSubmissionTransaction | null;
  readonly replacement: GasSubmissionTransaction | null;
  readonly completion: GasSubmissionOutcome | null;
}

function wireHash(value: unknown, field: string): string {
  const text = wireString(value, field);
  if (!/^0x[0-9a-f]{64}$/u.test(text)) throw new Refusal("invalid_response", field);
  return text;
}

function wireTransaction(value: unknown, field: string): GasSubmissionTransaction | null {
  if (value === null) return null;
  const data = record(value);
  return { sponsorNonce: wireUint(data.sponsorNonce, `${field}.sponsorNonce`), transactionHash: wireHash(data.transactionHash, `${field}.transactionHash`) };
}

function wireOutcome(value: unknown): GasSubmissionOutcome | null {
  if (value === null) return null;
  const data = record(value);
  switch (data.outcome) {
    case "consumed": return { outcome: "consumed" };
    case "reverted": return { outcome: "reverted", transactionHash: wireHash(data.transactionHash, "completion.transactionHash") };
    case "cancelled": return {
      outcome: "cancelled", transactionHash: wireHash(data.transactionHash, "completion.transactionHash"),
      blockNumber: wireUint(data.blockNumber, "completion.blockNumber"),
    };
    case "included": return {
      outcome: "included", transactionHash: wireHash(data.transactionHash, "completion.transactionHash"),
      blockNumber: wireUint(data.blockNumber, "completion.blockNumber"),
      sidCollected: wireUint(data.sidCollected, "completion.sidCollected"), paxSpent: wireUint(data.paxSpent, "completion.paxSpent"),
    };
    default: throw new Refusal("invalid_response", "completion.outcome");
  }
}

function wireStatus(payload: unknown): GasSubmissionStatus {
  const data = record(payload);
  const state = data.state;
  if (state !== "quoted" && state !== "pending" && state !== "replacing" && state !== "completed") {
    throw new Refusal("invalid_response", "state");
  }
  const status: GasSubmissionStatus = {
    state, deadline: wireUint(data.deadline, "deadline"),
    submission: wireTransaction(data.submission, "submission"), replacement: wireTransaction(data.replacement, "replacement"),
    completion: wireOutcome(data.completion),
  };
  if ((state === "completed") !== (status.completion !== null)) throw new Refusal("invalid_response", "completion");
  if (state === "replacing" && status.replacement === null) throw new Refusal("invalid_response", "replacement");
  if ((state === "pending" || state === "replacing") && status.submission === null) throw new Refusal("invalid_response", "submission");
  return status;
}

async function submissionRequest(
  route: "status" | "retry",
  config: GasStationConfig,
  identity: GasSubmissionIdentity,
  signal: AbortSignal | undefined,
  fetchImpl: typeof fetch,
): Promise<GasResult<GasSubmissionStatus>> {
  const prepared = result(() => {
    validateConfig(config);
    if (address(identity.sponsor, "sponsor") !== config.sponsor.toLowerCase()) throw new Refusal("sponsor_mismatch", "sponsor");
    address(identity.account, "account");
    uint(identity.quoteNonce, "quoteNonce");
    if (bytes(identity.relayerSignature, "relayerSignature").length !== 130) throw new Refusal("invalid_signature", "relayerSignature");
    let url: URL;
    try { url = new URL(gasStationQuoteUrl(config.quoteUrl)); } catch { throw new Refusal("invalid_value", "quoteUrl"); }
    if (!["https:", "http:"].includes(url.protocol) || url.username || url.password || url.hash || !url.pathname.endsWith("/quote")) {
      throw new Refusal("invalid_value", "quoteUrl");
    }
    url.pathname = `${url.pathname.slice(0, -"quote".length)}${route}`;
    return {
      url: url.toString(),
      body: JSON.stringify({
        sponsor: identity.sponsor, quoteNonce: identity.quoteNonce.toString(),
        account: identity.account, relayerSignature: identity.relayerSignature,
      }),
    };
  });
  if (!prepared.ok) return prepared;
  let response: Response;
  try {
    response = await fetchImpl(prepared.value.url, {
      method: "POST", headers: { "content-type": "application/json" }, body: prepared.value.body,
      signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000),
      redirect: "error", credentials: "omit", mode: "cors",
    });
  } catch {
    return { ok: false, refusal: { code: signal?.aborted ? "cancelled" : "unavailable", field: route } };
  }
  if (!response.ok) return { ok: false, refusal: { code: response.status >= 500 ? "unavailable" : "refused", field: route } };
  let payload: unknown;
  try { payload = await response.json(); } catch { return { ok: false, refusal: { code: "invalid_response", field: route } }; }
  return result(() => wireStatus(payload));
}

/** Reads the station's durable state of one submission identity; it stays readable after the quote's deadline. */
export function requestGasSubmissionStatus(
  config: GasStationConfig,
  identity: GasSubmissionIdentity,
  options: { readonly signal?: AbortSignal; readonly fetch?: typeof fetch } = {},
): Promise<GasResult<GasSubmissionStatus>> {
  return submissionRequest("status", config, identity, options.signal, options.fetch ?? fetch);
}

/** Asks the station to resume an already submitted identity from its durable bytes, without signing anything new. */
export function retryGasSubmission(
  config: GasStationConfig,
  identity: GasSubmissionIdentity,
  options: { readonly signal?: AbortSignal; readonly fetch?: typeof fetch } = {},
): Promise<GasResult<GasSubmissionStatus>> {
  return submissionRequest("retry", config, identity, options.signal, options.fetch ?? fetch);
}

export async function submitSponsoredGasBatch(
  config: GasStationConfig,
  batch: SponsoredBatch,
  accountSignature: string,
  relayerSignature: string,
  authorization: SignedEip7702Authorization,
  options: { readonly now?: bigint; readonly signal?: AbortSignal; readonly fetch?: typeof fetch } = {},
): Promise<GasResult<string>> {
  const prepared = result(() => {
    validateConfig(config);
    if (authorization.chainId !== config.chainId) throw new Refusal("chain_mismatch", "authorization.chainId");
    if (address(authorization.address, "authorization.address") !== address(config.paymaster, "paymaster")) {
      throw new Refusal("invalid_value", "authorization.address");
    }
    if (authorization.yParity !== 0 && authorization.yParity !== 1) throw new Refusal("invalid_signature", "authorization.yParity");
    const r = bytes(authorization.r, "authorization.r");
    const s = bytes(authorization.s, "authorization.s");
    if (r.length !== 64 || s.length !== 64) throw new Refusal("invalid_signature", "authorization");
    verifySignature(`0x${r}${s}${(authorization.yParity + 27).toString(16)}`, authorizationHash(authorization), batch.account);
    const call = sponsoredBatchCall(config, batch, accountSignature, relayerSignature, options.now);
    if (!call.ok) throw new Refusal(call.refusal.code, call.refusal.field);
    let url: URL;
    try { url = new URL(gasStationQuoteUrl(config.quoteUrl)); } catch { throw new Refusal("invalid_value", "quoteUrl"); }
    if (!["https:", "http:"].includes(url.protocol) || url.username || url.password || url.hash || url.search || !url.pathname.endsWith("/quote")) {
      throw new Refusal("invalid_value", "quoteUrl");
    }
    url.pathname = `${url.pathname.slice(0, -"quote".length)}submit`;
    return {
      url: url.toString(),
      body: JSON.stringify({ call: call.value, authorization, batch, accountSignature, relayerSignature },
        (_key, value: unknown) => typeof value === "bigint" ? value.toString() : value),
    };
  });
  if (!prepared.ok) return prepared;
  let response: Response;
  try {
    response = await (options.fetch ?? fetch)(prepared.value.url, {
      method: "POST", headers: { "content-type": "application/json" }, body: prepared.value.body,
      signal: options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000),
      redirect: "error", credentials: "omit", mode: "cors",
    });
  } catch {
    return { ok: false, refusal: { code: options.signal?.aborted ? "cancelled" : "unavailable", field: "submit" } };
  }
  if (!response.ok) return stationRefusal(response, "submit");
  let payload: unknown;
  try { payload = await response.json(); } catch { return { ok: false, refusal: { code: "invalid_response", field: "submit" } }; }
  return result(() => wireHash(record(payload).transactionHash, "transactionHash"));
}

export interface SponsoredConsent {
  readonly kind: "sponsored-eip7702";
  readonly account: string;
  readonly chainId: bigint;
  readonly delegate: string;
  readonly token: string;
  readonly symbol: "SID";
  readonly decimals: 6;
  readonly amount: bigint;
  readonly maximum: bigint;
  readonly amountDisplay: string;
  readonly maximumDisplay: string;
  readonly deadline: bigint;
  readonly batchDigest: string;
  readonly calls: readonly PrecompileCall[];
}

export function sponsoredConsent(config: GasStationConfig, batch: SponsoredBatch, now = BigInt(Math.floor(Date.now() / 1000))): GasResult<SponsoredConsent> {
  return result(() => {
    validateConfig(config); validateQuote(config, batch.quote, now);
    if (batch.chainId !== config.chainId) throw new Refusal("chain_mismatch", "chainId");
    const consentDigest = sponsoredBatchDigest(batch);
    if (!consentDigest.ok) throw new Refusal(consentDigest.refusal.code, consentDigest.refusal.field);
    const display = (value: bigint) => `${value / 1_000_000n}.${(value % 1_000_000n).toString().padStart(6, "0")} SID`;
    return Object.freeze({ kind: "sponsored-eip7702", account: address(batch.account, "account"),
      chainId: batch.chainId, delegate: address(config.paymaster, "paymaster"), token: config.token,
      symbol: "SID", decimals: 6, amount: batch.quote.tokenAmount, maximum: batch.quote.maxTokenAmount,
      amountDisplay: display(batch.quote.tokenAmount), maximumDisplay: display(batch.quote.maxTokenAmount),
      deadline: batch.quote.deadline, batchDigest: consentDigest.value,
      calls: Object.freeze(batch.calls.map(call => Object.freeze({ ...call }))) });
  });
}

export async function discoverSponsoredNonce(config: GasStationConfig, account: string,
  request: (args: { readonly method: string; readonly params: readonly unknown[] }) => Promise<unknown>): Promise<GasResult<bigint>> {
  try {
    validateConfig(config); const target = address(account, "account");
    const chain = await request({ method: "eth_chainId", params: [] });
    if (typeof chain !== "string" || !/^0x[0-9a-fA-F]+$/u.test(chain) || BigInt(chain) !== config.chainId)
      throw new Refusal("chain_mismatch", "chainId");
    const head = record(await request({ method: "eth_getBlockByNumber", params: ["latest", false] }));
    const block = { blockHash: wireHash(head.hash, "blockHash"), requireCanonical: true };
    if (/^0x0{64}$/u.test(block.blockHash)) throw new Refusal("invalid_response", "blockHash");
    const code = await request({ method: "eth_getCode", params: [target, block] });
    if (typeof code !== "string" || !/^0x(?:[0-9a-fA-F]{2})*$/u.test(code) || code.length > 49_154)
      throw new Refusal("invalid_response", "code");
    const call = { to: target, data: abiSelector("nonce()") };
    let params: readonly unknown[] = [call, block];
    if (code === "0x") {
      const implementation = await request({ method: "eth_getCode", params: [config.paymaster, block] });
      if (typeof implementation !== "string" || !/^0x(?:[0-9a-fA-F]{2})+$/u.test(implementation)
          || implementation.length > 49_154 || implementation.toLowerCase().startsWith("0xef0100"))
        throw new Refusal("replay_state_unavailable", "paymasterCode");
      params = [call, block, { [target]: { code: implementation } }];
    } else if (code.toLowerCase() !== `0xef0100${config.paymaster.slice(2).toLowerCase()}`) {
      throw new Refusal("incompatible_delegation", "delegation");
    }
    const nonce = await request({ method: "eth_call", params });
    if (typeof nonce !== "string" || !/^0x[0-9a-fA-F]{64}$/u.test(nonce))
      throw new Refusal("replay_state_unavailable", "nonce");
    return { ok: true, value: BigInt(nonce) };
  } catch (error) {
    return { ok: false, refusal: error instanceof Refusal ? { code: error.code, field: error.field }
      : { code: "replay_state_unavailable", field: "eth_call" } };
  }
}


export type NativeFeePreference =
  | { readonly kind: "native-fee-preference"; readonly state: "inactive" | "unavailable"; readonly reason: string }
  | { readonly kind: "native-fee-preference"; readonly state: "available"; readonly chainId: 125n;
      readonly blockHash: string; readonly blockNumber: bigint; readonly denom: string; readonly symbol: "SID";
      readonly decimals: 6; readonly rate: string; readonly rateUpdateHeight: bigint;
      readonly currentDenom: string; readonly call: PrecompileCall };

function nativeString(value: unknown, headWords: number): string {
  if (typeof value !== "string" || !/^0x(?:[0-9a-fA-F]{64})+$/u.test(value) || value.length > 2_050)
    throw new Refusal("invalid_response", "nativeFeeAbi");
  const raw = value.slice(2);
  const offset = Number(BigInt(`0x${raw.slice(0, 64)}`));
  if (offset !== headWords * 32 || raw.length < (offset + 32) * 2)
    throw new Refusal("invalid_response", "nativeFeeAbi");
  const size = Number(BigInt(`0x${raw.slice(offset * 2, offset * 2 + 64)}`));
  if (!Number.isSafeInteger(size) || size < 1 || size > 256
      || raw.length !== (offset + 32 + Math.ceil(size / 32) * 32) * 2
      || !/^0*$/u.test(raw.slice((offset + 32 + size) * 2)))
    throw new Refusal("invalid_response", "nativeFeeAbi");
  const data = raw.slice((offset + 32) * 2, (offset + 32 + size) * 2);
  const text = new TextDecoder("utf-8", { fatal: true }).decode(Uint8Array.from(data.match(/../gu) ?? [], byte => parseInt(byte, 16)));
  if (!/^[a-zA-Z][a-zA-Z0-9/:._-]{2,255}$/u.test(text)) throw new Refusal("invalid_response", "denom");
  return text;
}

export async function readNativeFeePreference(account: string, options: {
  readonly restUrl: string;
  readonly request: (args: { readonly method: string; readonly params: readonly unknown[] }) => Promise<unknown>;
  readonly fetch?: typeof fetch;
  readonly signal?: AbortSignal;
}): Promise<NativeFeePreference> {
  const inactive = (reason: string): NativeFeePreference => ({ kind: "native-fee-preference", state: "inactive", reason });
  try {
    const target = address(account, "account");
    if (await options.request({ method: "eth_chainId", params: [] }) !== "0x7d") return inactive("chain_mismatch");
    const head = record(await options.request({ method: "eth_getBlockByNumber", params: ["finalized", false] }));
    const blockHash = wireHash(head.hash, "blockHash");
    if (/^0x0{64}$/u.test(blockHash)) throw new Refusal("invalid_response", "blockHash");
    if (typeof head.number !== "string" || head.number.length > 18 || !/^0x[0-9a-f]+$/u.test(head.number)) throw new Refusal("invalid_response", "blockNumber");
    const height = BigInt(head.number); const block = { blockHash, requireCanonical: true };
    const base = new URL(options.restUrl);
    if (!["https:", "http:"].includes(base.protocol) || base.username || base.password || base.search || base.hash)
      throw new Refusal("invalid_value", "restUrl");
    const read = async (path: string): Promise<Record<string, unknown>> => {
      const response = await (options.fetch ?? fetch)(new URL(path, base), { redirect: "error",
        headers: { "x-cosmos-block-height": height.toString() },
        signal: options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000) });
      if (!response.ok || (response.headers.get("x-cosmos-block-height") ?? response.headers.get("grpc-metadata-x-cosmos-block-height")) !== height.toString()
          || Number(response.headers.get("content-length") ?? 0) > 65_536 || response.body === null)
        throw new Refusal("unavailable", "governedState");
      const reader = response.body.getReader(); let size = 0; const chunks: Uint8Array[] = [];
      try { for (;;) { const next = await reader.read(); if (next.done) break;
        size += next.value.length; if (size > 65_536) throw new Refusal("invalid_response", "governedState"); chunks.push(next.value); }
      } finally { await reader.cancel(); }
      const payload = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { payload.set(chunk, offset); offset += chunk.length; }
      return record(JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(payload)));
    };
    const applied = await read("/cosmos/upgrade/v1beta1/applied_plan/v6.7");
    const upgrade = wireUint(applied.height, "upgradeHeight");
    if (upgrade === 0n || upgrade > height) return inactive("upgrade_not_applied");
    const param = async (key: string): Promise<unknown> => {
      const body = record((await read(`/cosmos/params/v1beta1/params?subspace=evm&key=${key}`)).param);
      if (body.subspace !== "evm" || body.key !== key || typeof body.value !== "string")
        throw new Refusal("invalid_response", "governedParameter");
      return JSON.parse(body.value);
    };
    const enabled = await param("KeyFeeTokenEnabled");
    if (typeof enabled !== "boolean") throw new Refusal("invalid_response", "feeTokenEnabled");
    if (!enabled) return inactive("fee_token_disabled");
    const bridge = "0x0000000000000000000000000000000000001016";
    const denom = nativeString(await options.request({ method: "eth_call", params: [{ to: bridge,
      data: encodeAbiCall("getCap", ["uint64", "address"], [0x534f4c414e41n, SIDIORA_TOKEN]) }, block] }), 4);
    if (!/^factory\/[^/]+\/usid$/u.test(denom)) return inactive("sid_registration_mismatch");
    const allowed = await param("KeyAllowedFeeDenoms");
    if (!Array.isArray(allowed) || allowed.length > 256) throw new Refusal("invalid_response", "allowedFeeDenoms");
    const matches = allowed.map(record).filter(entry => entry.denom === denom);
    if (matches.length !== 1) return inactive("sid_not_allowed");
    const entry = matches[0]!;
    if (typeof entry.rate !== "string" || !/^(0|[1-9][0-9]*)(?:\.[0-9]{1,18})?$/u.test(entry.rate)
        || entry.rate.length > 98 || BigInt(entry.rate.replace(".", "")) === 0n) return inactive("invalid_rate");
    const updated = wireUint(typeof entry.rate_update_height === "number" && Number.isSafeInteger(entry.rate_update_height)
      ? String(entry.rate_update_height) : entry.rate_update_height, "rateUpdateHeight");
    const ageValue = await param("KeyMaxFeeTokenRateAge");
    const maxAge = wireUint(typeof ageValue === "number" && Number.isSafeInteger(ageValue) ? String(ageValue) : ageValue, "maxRateAge");
    if (maxAge === 0n || updated > height || height - updated > maxAge) return inactive("stale_rate");
    const metadata = record((await read(`/cosmos/bank/v1beta1/denoms_metadata/${encodeURIComponent(denom)}`)).metadata);
    if (metadata.base !== denom || metadata.display !== "SID" || metadata.symbol !== "SID" || metadata.name !== "Sidiora"
        || !Array.isArray(metadata.denom_units) || metadata.denom_units.length !== 2) return inactive("sid_metadata_mismatch");
    const units = metadata.denom_units.map(record);
    if (!units.some(unit => unit.denom === denom && unit.exponent === 0)
        || !units.some(unit => unit.denom === "SID" && unit.exponent === 6)) return inactive("sid_metadata_mismatch");
    const precompile = "0x0000000000000000000000000000000000001018";
    const currentDenom = nativeString(await options.request({ method: "eth_call", params: [{ to: precompile,
      data: encodeAbiCall("getFeeDenom", ["address"], [target]) }, block] }), 1);
    const call = { to: precompile, data: encodeAbiCall("setFeeDenom", ["string"], [denom]), value: 0n };
    if (await options.request({ method: "eth_call", params: [{ from: target, to: call.to, data: call.data }, block] }) !== "0x")
      throw new Refusal("invalid_response", "feePreferenceSimulation");
    const canonical = record(await options.request({ method: "eth_getBlockByNumber", params: [head.number, false] }));
    if (wireHash(canonical.hash, "blockHash") !== blockHash) throw new Refusal("unavailable", "canonicalBlock");
    return { kind: "native-fee-preference", state: "available", chainId: 125n, blockHash, blockNumber: height,
      denom, symbol: "SID", decimals: 6, rate: entry.rate, rateUpdateHeight: updated, currentDenom, call };
  } catch (error) {
    return { kind: "native-fee-preference", state: "unavailable", reason: error instanceof Refusal ? error.field : "governed_state_unavailable" };
  }
}
