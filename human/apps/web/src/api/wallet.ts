import { gasStation, GasStationError, ModuleError, type GasSubmissionStore, type SponsoredSubmissionEvidence } from "@paxeer/wallet";
import { sidioraStationMetadata } from "./gas-station.ts";
import { windowWalletProvider } from "../journeys/custody/handoff.ts";
import { copyEntry } from "../../copy/runtime.ts";
import { parseFeeAmount } from "../auth/native-fee-budget.ts";
import {
  SIDIORA_DECIMALS, SIDIORA_TOKEN,
  requestSidioraGasQuote, sponsoredBatchDigest,
  sendPrecompileCall, type PrecompileCall, type SidioraGasQuote,
} from "./sdk.ts";

export type PrecompileSendOutcome =
  | Readonly<{ outcome: "sent"; transactionHash: string }>
  | Readonly<{ outcome: "cancelled" | "rejected" | "unavailable" | "failed"; detail?: string }>;

const EVM_ADDRESS = /^0x[0-9a-fA-F]{40}$/u;
const USER_REJECTED_REQUEST = 4001;
const UNAUTHORIZED_REQUEST = 4100;
const PROVIDER_DISCONNECTED = 4900;
const CHAIN_DISCONNECTED = 4901;

export function walletSendFailure(error: unknown): PrecompileSendOutcome {
  const code = typeof error === "object" && error !== null && "code" in error ? error.code : undefined;
  const detail = error instanceof Error ? error.message : undefined;
  if (error instanceof GasStationError) {
    return { outcome: error.refusal.code === "unavailable" ? "unavailable" : error.refusal.code === "cancelled" ? "cancelled" : "rejected" };
  }
  if (error instanceof ModuleError) return { outcome: error.code === "unavailable" ? "unavailable" : "rejected" };
  if (code === USER_REJECTED_REQUEST) {
    return { outcome: "cancelled" };
  }
  if (code === UNAUTHORIZED_REQUEST) {
    return { outcome: "rejected" };
  }
  if (code === PROVIDER_DISCONNECTED || code === CHAIN_DISCONNECTED) {
    return { outcome: "unavailable" };
  }
  return detail === undefined ? { outcome: "failed" } : { outcome: "failed", detail };
}

/** Asks the Paxeer wallet for its first account. */
export async function connectWalletAccount(): Promise<string | undefined> {
  const wallet = windowWalletProvider();
  if (wallet === undefined) {
    return undefined;
  }
  const response = await wallet.request({ method: "eth_requestAccounts" });
  const first: unknown = Array.isArray(response) ? (response as readonly unknown[])[0] : undefined;
  return typeof first === "string" && EVM_ADDRESS.test(first) ? first.toLowerCase() : undefined;
}

/** Sends one SDK-built precompile call through the Paxeer wallet. */
export async function sendWalletPrecompileCall(from: string, build: () => PrecompileCall): Promise<PrecompileSendOutcome> {
  const wallet = windowWalletProvider();
  if (wallet === undefined) {
    return { outcome: "unavailable" };
  }
  try {
    const preference = walletFeePreference();
    if (preference.currency === "sidiora") {
      if (preference.maximum === undefined) return { outcome: "rejected" };
      const recovered = await resumeWalletSponsoredSubmission(from);
      if (recovered !== undefined) return recovered;
      const metadata = await sidioraStationMetadata();
      if (metadata === undefined) return { outcome: "unavailable" };
      const chainId = rpcQuantity(await wallet.request({ method: "eth_chainId" }));
      if (chainId.toString() !== metadata.chainId) return { outcome: "rejected" };
      const module = gasStation(wallet, { chainId, sponsor: metadata.sponsor, paymaster: metadata.paymaster });
      const nonce = await module.batchNonce(from);
      const call = build();
      const gas = rpcQuantity(await wallet.request({ method: "eth_estimateGas", params: [{
        from, to: call.to, data: call.data, value: `0x${call.value.toString(16)}`,
      }] }));
      const price = rpcQuantity(await wallet.request({ method: "eth_gasPrice" }));
      const quoted = await requestSidioraGasQuote({ account: from, nonce, calls: [call], maxTokenAmount: preference.maximum, gasCost: gas * price }, chainId);
      if (!quoted.ok) return quoted.failure;
      const review = sidioraQuotePresentation(quoted.value);
      if (review === undefined) return { outcome: "rejected" };
      const consent = typeof window !== "undefined" && window.confirm(`${review.consent}\nAccount: ${quoted.value.batch.account}\nNetwork: ${quoted.value.batch.chainId}`);
      return await sendWalletSponsoredBatch(quoted.value, consent ? review.identity : undefined);
    }
    const transactionHash = await sendPrecompileCall(wallet, from, build());
    return { outcome: "sent", transactionHash };
  } catch (error) {
    return walletSendFailure(error);
  }
}


export type WalletFeePreference = Readonly<{ currency: "paxeer" | "sidiora"; maximum?: bigint }>;
const FEE_PREFERENCE = "paxeer.wallet.fee";
let feePreference: WalletFeePreference = { currency: "paxeer" };

export function walletFeePreference(): WalletFeePreference {
  if (typeof window === "undefined") return feePreference;
  try {
    const stored = window.sessionStorage.getItem(FEE_PREFERENCE);
    if (stored === null) return feePreference;
    if (stored === "paxeer") return { currency: "paxeer" };
    const maximum = parseFeeAmount(stored, SIDIORA_DECIMALS);
    return maximum === undefined ? { currency: "sidiora" } : { currency: "sidiora", maximum };
  } catch {
    return feePreference;
  }
}

export function chooseWalletFee(currency: WalletFeePreference["currency"], maximum: string): boolean {
  const parsed = parseFeeAmount(maximum, SIDIORA_DECIMALS);
  feePreference = currency === "paxeer" ? { currency } : parsed === undefined ? { currency } : { currency, maximum: parsed };
  if (typeof window !== "undefined") {
    try { window.sessionStorage.setItem(FEE_PREFERENCE, currency === "paxeer" ? currency : maximum); } catch { return currency === "paxeer" || parsed !== undefined; }
  }
  return currency === "paxeer" || parsed !== undefined;
}

export function sidioraAmount(amount: bigint): string {
  const scale = 10n ** BigInt(SIDIORA_DECIMALS);
  return `${(amount / scale).toString()}.${(amount % scale).toString().padStart(SIDIORA_DECIMALS, "0")}`;
}

export function sidioraQuotePresentation(quoted: SidioraGasQuote, now = BigInt(Math.floor(Date.now() / 1000))) {
  const quote = quoted.batch.quote;
  const digest = sponsoredBatchDigest(quoted.batch);
  if (!digest.ok || quote.token.toLowerCase() !== SIDIORA_TOKEN.toLowerCase()
    || quote.decimals !== SIDIORA_DECIMALS || quote.tokenAmount <= 0n
    || quote.tokenAmount > quote.maxTokenAmount || quote.deadline <= now
    || quote.deadline > 8_640_000_000_000n) return undefined;
  const amount = sidioraAmount(quote.tokenAmount);
  const maximum = sidioraAmount(quote.maxTokenAmount);
  const deadline = new Date(Number(quote.deadline) * 1000).toUTCString();
  const consent = copyEntry("gas.sidiora.consent").message
    .replace("{amount}", amount).replace("{maximum}", maximum).replace("{deadline}", deadline);
  return { amount, maximum, deadline, consent, identity: `${digest.value}:${quoted.paymaster.toLowerCase()}:${quoted.relayerSignature.toLowerCase()}` };
}

function rpcQuantity(value: unknown): bigint {
  if (typeof value !== "string" || !/^0x[0-9a-fA-F]+$/u.test(value)) throw new Error(copyEntry("gas.sidiora.failed").message);
  return BigInt(value);
}

const SPONSORED_CONTEXT = "layerx:private-sponsored:v1:";

export function sponsoredWalletModule(quoted: SidioraGasQuote, wallet: NonNullable<ReturnType<typeof windowWalletProvider>>, submissionStore?: GasSubmissionStore) {
  return gasStation(wallet, {
    chainId: quoted.batch.chainId, sponsor: quoted.batch.quote.sponsor, paymaster: quoted.paymaster,
    ...(submissionStore === undefined ? {} : { submissionStore }),
  });
}

function browserSubmissionStore(): Storage {
  if (typeof window === "undefined") throw new Error("Durable sponsored storage is unavailable");
  return window.localStorage;
}

function sponsoredContextKey(account: string): string {
  if (!EVM_ADDRESS.test(account)) throw new Error("Invalid sponsored account");
  return SPONSORED_CONTEXT + account.toLowerCase();
}

function statusOutcome(report: SponsoredSubmissionEvidence): PrecompileSendOutcome {
  if (report.status === "cancelled" || report.status === "reverted") return { outcome: "rejected" };
  return report.tx_hash === null ? { outcome: "unavailable" } : { outcome: "sent", transactionHash: report.tx_hash };
}

export async function resumeWalletSponsoredSubmission(account: string): Promise<PrecompileSendOutcome | undefined> {
  const wallet = windowWalletProvider();
  if (wallet === undefined) return { outcome: "unavailable" };
  try {
    const store = browserSubmissionStore();
    const raw = store.getItem(sponsoredContextKey(account));
    if (raw === null) return undefined;
    if (raw.length > 1024) return { outcome: "rejected" };
    const value: unknown = JSON.parse(raw);
    if (typeof value !== "object" || value === null || Array.isArray(value)) return { outcome: "rejected" };
    const context = value as Record<string, unknown>;
    if (Object.keys(context).length !== 6 || context.version !== 1 || context.account !== account.toLowerCase()
      || typeof context.chainId !== "string" || !/^[1-9][0-9]*$/.test(context.chainId)
      || BigInt(context.chainId) >= 1n << 256n || typeof context.sponsor !== "string" || !EVM_ADDRESS.test(context.sponsor)
      || typeof context.paymaster !== "string" || !EVM_ADDRESS.test(context.paymaster)
      || typeof context.identity !== "string" || !/^0x[0-9a-fA-F]{64}:0x[0-9a-fA-F]{40}:0x[0-9a-fA-F]{130}$/.test(context.identity)) return { outcome: "rejected" };
    const module = gasStation(wallet, { chainId: BigInt(context.chainId), sponsor: context.sponsor, paymaster: context.paymaster, submissionStore: store });
    const retained = await module.pending(account);
    if (retained === null) return { outcome: "unavailable" };
    const digest = sponsoredBatchDigest(retained.batch);
    if (!digest.ok || context.identity !== `${digest.value}:${context.paymaster.toLowerCase()}:${retained.relayerSignature.toLowerCase()}`) return { outcome: "rejected" };
    const report = await module.resume(retained.batch, retained.relayerSignature);
    if (report.status !== "pending") store.removeItem(sponsoredContextKey(account));
    return statusOutcome(report);
  } catch (error) { return walletSendFailure(error); }
}

export async function sendWalletSponsoredBatch(quoted: SidioraGasQuote, consentIdentity: string | undefined): Promise<PrecompileSendOutcome> {
  if (consentIdentity === undefined) return { outcome: "cancelled" };
  const snapshot = structuredClone(quoted);
  const review = sidioraQuotePresentation(snapshot);
  if (review === undefined || review.identity !== consentIdentity) return { outcome: "rejected" };
  const wallet = windowWalletProvider();
  if (wallet === undefined) return { outcome: "unavailable" };
  try {
    const storage = browserSubmissionStore();
    const module = sponsoredWalletModule(snapshot, wallet, storage);
    const pending = await module.pending(snapshot.batch.account);
    if (pending !== null) {
      const digest = sponsoredBatchDigest(pending.batch);
      const expected = sponsoredBatchDigest(snapshot.batch);
      if (!digest.ok || !expected.ok || digest.value !== expected.value || pending.relayerSignature.toLowerCase() !== snapshot.relayerSignature.toLowerCase()) return { outcome: "rejected" };
      return statusOutcome(await module.resume(pending.batch, pending.relayerSignature));
    }
    const key = sponsoredContextKey(snapshot.batch.account);
    const context = { version: 1, account: snapshot.batch.account.toLowerCase(), chainId: snapshot.batch.chainId.toString(),
      sponsor: snapshot.batch.quote.sponsor.toLowerCase(), paymaster: snapshot.paymaster.toLowerCase(), identity: consentIdentity };
    const existing = storage.getItem(key);
    if (existing !== null && existing !== JSON.stringify(context)) return { outcome: "rejected" };
    storage.setItem(key, JSON.stringify(context));
    const transactionHash = await module.submitFirstUse(snapshot.batch, snapshot.relayerSignature, {
      confirm: consent => {
        const current = sidioraQuotePresentation(snapshot);
        const digest = sponsoredBatchDigest(snapshot.batch);
        return current !== undefined && current.identity === consentIdentity && digest.ok && consent.batchDigest === digest.value;
      },
    });
    return { outcome: "sent", transactionHash };
  } catch (error) {
    return walletSendFailure(error);
  }
}
