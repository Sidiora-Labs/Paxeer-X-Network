/* ══════════════════════════════════════════════════════════════════════
   Error classifier

   Takes ANY thrown thing — a wagmi/viem error, a fetch Response, an
   AbortError, a string, undefined, an object literal, a class instance
   — and returns a `UserFacingError` ready to render. Never throws.

   Order of pattern matching matters: more specific patterns must come
   before more general ones. The classifier returns the first match;
   the catch-all is `UNKNOWN`.
   ══════════════════════════════════════════════════════════════════════ */

import { ERROR_CATALOG, getCatalogEntry } from "./catalog";
import type { ErrorCode, ErrorContext, UserFacingError } from "./types";

/* ── helpers ────────────────────────────────────────────────────── */

function asString(v: unknown): string {
  if (typeof v === "string") return v;
  if (v == null) return "";
  if (typeof v === "object") {
    const o = v as Record<string, unknown>;
    if (typeof o.message === "string") return o.message;
    if (typeof o.shortMessage === "string") return o.shortMessage;
    if (typeof o.reason === "string") return o.reason;
    try {
      return JSON.stringify(v);
    } catch {
      return String(v);
    }
  }
  return String(v);
}

function getErrorName(v: unknown): string {
  if (v && typeof v === "object") {
    const n = (v as { name?: unknown }).name;
    if (typeof n === "string") return n;
  }
  return "";
}

function getStatusCode(v: unknown): number | null {
  if (!v || typeof v !== "object") return null;
  const o = v as Record<string, unknown>;
  if (typeof o.status === "number") return o.status;
  if (typeof o.statusCode === "number") return o.statusCode;
  // viem RPC errors carry `code` for JSON-RPC error codes (negative ints)
  // and `details.status` sometimes; we keep the JSON-RPC code separate
  // because it overlaps with HTTP status codes.
  return null;
}

function getRpcCode(v: unknown): number | null {
  if (!v || typeof v !== "object") return null;
  const o = v as Record<string, unknown>;
  if (typeof o.code === "number") return o.code;
  if (
    o.cause &&
    typeof o.cause === "object" &&
    typeof (o.cause as Record<string, unknown>).code === "number"
  ) {
    return (o.cause as Record<string, unknown>).code as number;
  }
  return null;
}

function joinFingerprint(parts: Array<string | number | undefined | null>): string[] {
  return parts.filter((p): p is string | number => p != null && p !== "").map(String);
}

/* ── pattern matchers ───────────────────────────────────────────── */

interface MatchInput {
  raw: unknown;
  message: string;
  lower: string;
  name: string;
  status: number | null;
  rpcCode: number | null;
}

type Matcher = (i: MatchInput) => ErrorCode | null;

const matchers: Matcher[] = [
  // Abort / cancellation — must come first; AbortError is benign.
  ({ name, lower }) => {
    if (name === "AbortError") return "RUNTIME_ABORTED";
    if (lower.includes("aborted") && lower.includes("user")) return "RUNTIME_ABORTED";
    if (lower === "the operation was aborted." || lower === "the user aborted a request.") {
      return "RUNTIME_ABORTED";
    }
    return null;
  },

  // Wallet / signing
  ({ raw, lower, name, rpcCode }) => {
    if (name === "UserRejectedRequestError" || rpcCode === 4001) return "WALLET_USER_REJECTED";
    if (lower.includes("user rejected") || lower.includes("user denied")) {
      return "WALLET_USER_REJECTED";
    }
    if (raw && typeof raw === "object" && (raw as { code?: unknown }).code === "ACTION_REJECTED") {
      return "WALLET_USER_REJECTED";
    }
    return null;
  },
  ({ name, lower }) => {
    if (name === "ChainMismatchError" || name === "SwitchChainError") return "WALLET_WRONG_NETWORK";
    if (lower.includes("wrong network") || lower.includes("unsupported chain")) {
      return "WALLET_WRONG_NETWORK";
    }
    if (lower.includes("does not match the target chain")) return "WALLET_WRONG_NETWORK";
    return null;
  },
  ({ name, lower }) => {
    if (name === "ProviderDisconnectedError" || name === "ConnectorNotFoundError") {
      return "WALLET_PROVIDER_UNAVAILABLE";
    }
    if (lower.includes("no wallet") || lower.includes("provider not found")) {
      return "WALLET_PROVIDER_UNAVAILABLE";
    }
    if (lower.includes("not connected") && lower.includes("wallet")) {
      return "WALLET_NOT_CONNECTED";
    }
    return null;
  },
  ({ lower }) => {
    if (lower.includes("insufficient funds") || lower.includes("insufficient balance")) {
      return "WALLET_INSUFFICIENT_FUNDS";
    }
    return null;
  },
  ({ name, lower }) => {
    if (name === "TimeoutError" || lower.includes("wallet timeout")) return "WALLET_TIMEOUT";
    return null;
  },
  ({ lower }) => {
    // Embedded wallet API (connect.paxportwallet.com) 5xx. viem wraps
    // the PaxeerWalletError in ContractFunctionExecutionError, which
    // the revert rule below would mislabel "Transaction reverted" —
    // nothing reached the chain, so match the SDK's message first.
    if (lower.includes("paxeerwalleterror") || /request failed: 5\d\d/.test(lower)) {
      return "WALLET_PROVIDER_UNAVAILABLE";
    }
    return null;
  },

  // RPC / on-chain
  ({ name, lower, rpcCode }) => {
    if (name === "ContractFunctionRevertedError" || name === "ContractFunctionExecutionError") {
      return "RPC_REVERT";
    }
    if (lower.includes("execution reverted") || lower.includes("vm exception")) {
      return "RPC_REVERT";
    }
    // waitForTx throws "Transaction reverted (hash: 0x…)" on a
    // reverted receipt — without this it falls through to UNKNOWN.
    if (lower.includes("transaction reverted")) return "RPC_REVERT";
    // CALL_EXCEPTION is ethers
    if (rpcCode === -32000 && lower.includes("revert")) return "RPC_REVERT";
    return null;
  },
  ({ name, lower }) => {
    if (name === "EstimateGasExecutionError") return "RPC_GAS_ESTIMATION_FAILED";
    if (lower.includes("gas required exceeds") || lower.includes("cannot estimate gas")) {
      return "RPC_GAS_ESTIMATION_FAILED";
    }
    return null;
  },
  ({ lower }) => {
    if (lower.includes("nonce too low")) return "RPC_NONCE_TOO_LOW";
    if (lower.includes("replacement transaction underpriced")) return "RPC_REPLACEMENT_UNDERPRICED";
    if (lower.includes("transaction was not mined") || lower.includes("transaction timed out")) {
      return "RPC_TX_TIMEOUT";
    }
    return null;
  },
  ({ rpcCode, lower }) => {
    // -32005 / 429 mean rate limited from many JSON-RPC providers
    if (rpcCode === -32005) return "RPC_RATE_LIMITED";
    if (lower.includes("rate limit") && lower.includes("rpc")) return "RPC_RATE_LIMITED";
    if (lower.includes("could not coalesce") || lower.includes("connection failed")) {
      return "RPC_DISCONNECTED";
    }
    return null;
  },

  // HTTP / API
  ({ status, lower }) => {
    if (status === 408 || lower.includes("request timeout")) return "API_TIMEOUT";
    if (status === 401) return "API_UNAUTHORIZED";
    if (status === 403) return "API_FORBIDDEN";
    if (status === 404) return "API_NOT_FOUND";
    if (status === 429) return "API_RATE_LIMITED";
    if (status === 502 || status === 504) return "API_BAD_GATEWAY";
    if (status === 503) return "API_OFFLINE";
    if (status && status >= 500) return "API_SERVER_ERROR";
    if (status === 400 || status === 422) return "API_BAD_REQUEST";
    return null;
  },
  ({ name, lower }) => {
    if (name === "TypeError" && lower.includes("failed to fetch")) return "API_OFFLINE";
    if (lower.includes("networkerror when attempting to fetch")) return "API_OFFLINE";
    if (lower.includes("load failed") && lower.includes("network")) return "API_OFFLINE";
    return null;
  },
  ({ name, lower }) => {
    if (name === "ZodError" || lower.includes("invalid response shape")) return "API_PAYLOAD_INVALID";
    if (lower.includes("unexpected token") && lower.includes("json")) return "API_PAYLOAD_INVALID";
    return null;
  },

  // WebSocket
  ({ lower }) => {
    if (lower.includes("websocket") && lower.includes("close")) return "WS_DISCONNECTED";
    if (lower.includes("websocket") && lower.includes("fail")) return "WS_CONNECT_FAILED";
    if (lower.includes("connection lost") || lower.includes("ws closed")) return "WS_DISCONNECTED";
    return null;
  },

  // App / runtime
  ({ name, lower }) => {
    if (name === "ChunkLoadError") return "RUNTIME_CHUNK_LOAD";
    if (lower.includes("loading chunk") && lower.includes("failed")) return "RUNTIME_CHUNK_LOAD";
    return null;
  },
  ({ name, lower }) => {
    if (name === "QuotaExceededError") return "RUNTIME_QUOTA_EXCEEDED";
    if (lower.includes("quota") && lower.includes("exceed")) return "RUNTIME_QUOTA_EXCEEDED";
    if (name === "NotAllowedError" || lower.includes("permission denied")) {
      return "RUNTIME_PERMISSION_DENIED";
    }
    if (lower.includes("hydration") && (lower.includes("mismatch") || lower.includes("failed"))) {
      return "RUNTIME_HYDRATION";
    }
    return null;
  },

  // Trade / position lifecycle (string-matched against contract revert reasons)
  ({ lower }) => {
    if (lower.includes("slippage")) return "TRADE_SLIPPAGE_EXCEEDED";
    if (lower.includes("market halted") || lower.includes("market paused")) {
      return "TRADE_MARKET_HALTED";
    }
    if (lower.includes("insufficient liquidity") || lower.includes("not enough liquidity")) {
      return "TRADE_INSUFFICIENT_LIQUIDITY";
    }
    if (lower.includes("liquidatable") || lower.includes("undercollateralized")) {
      return "TRADE_POSITION_LIQUIDATABLE";
    }
    if (lower.includes("invalid trigger") || lower.includes("invalid tp") || lower.includes("invalid sl")) {
      return "TRADE_TP_SL_INVALID";
    }
    return null;
  },
];

/* ── public API ─────────────────────────────────────────────────── */

/**
 * Classify any thrown thing into a `UserFacingError`. Never throws.
 *
 * @param raw   The thing that was thrown.
 * @param ctx   Optional context tag — passed through unchanged.
 */
export function classifyError(raw: unknown, ctx?: ErrorContext): UserFacingError {
  const message = asString(raw);
  const lower = message.toLowerCase();
  const name = getErrorName(raw);
  const status = getStatusCode(raw);
  const rpcCode = getRpcCode(raw);

  let code: ErrorCode = "UNKNOWN";
  for (const m of matchers) {
    const r = m({ raw, message, lower, name, status, rpcCode });
    if (r) {
      code = r;
      break;
    }
  }

  const entry = getCatalogEntry(code);
  return {
    ...entry,
    context: ctx,
    cause: raw,
    fingerprint: joinFingerprint([code, ctx, name, status]),
  };
}

/**
 * Build a `UserFacingError` directly from a catalog code — useful
 * when a UI component knows exactly what's wrong (e.g. validation).
 */
export function makeError(code: ErrorCode, ctx?: ErrorContext, cause?: unknown): UserFacingError {
  const entry = ERROR_CATALOG[code];
  return {
    ...entry,
    context: ctx,
    cause,
    fingerprint: joinFingerprint([code, ctx]),
  };
}

/**
 * Type guard so callers can detect already-classified errors and
 * avoid re-classifying them.
 */
export function isUserFacingError(v: unknown): v is UserFacingError {
  return (
    !!v &&
    typeof v === "object" &&
    typeof (v as Record<string, unknown>).code === "string" &&
    typeof (v as Record<string, unknown>).title === "string" &&
    typeof (v as Record<string, unknown>).message === "string"
  );
}
