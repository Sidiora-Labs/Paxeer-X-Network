/* ══════════════════════════════════════════════════════════════════════
   paxeer-network error catalog

   Every entry maps a stable `ErrorCode` to the user-facing copy and
   metadata. This file is the ONLY place where error wording lives —
   touch this to adjust tone, add a fix suggestion, or change severity.

   Voice: calm, brand-aligned, never robotic. Each `title` is 3–6
   words, sentence case. Each `message` is one short sentence in plain
   English. Each `suggestedFix` is a concrete action the user can do
   right now or `null` if there's nothing they can do (in which case we
   show the "Report" button instead).
   ══════════════════════════════════════════════════════════════════════ */

import type { ErrorCode, UserFacingError } from "./types";

type CatalogEntry = Omit<UserFacingError, "context" | "cause" | "fingerprint">;

export const ERROR_CATALOG: Record<ErrorCode, CatalogEntry> = {
  // ── Wallet / signing ──────────────────────────────────────────────
  WALLET_NOT_CONNECTED: {
    code: "WALLET_NOT_CONNECTED",
    title: "Wallet not connected",
    message: "You need a connected wallet to do this.",
    suggestedFix: "Connect your wallet to continue.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  WALLET_USER_REJECTED: {
    code: "WALLET_USER_REJECTED",
    title: "Request cancelled",
    message: "You declined the wallet prompt — no transaction was sent.",
    suggestedFix: "Try again and approve the prompt in your wallet.",
    severity: "info",
    isReportable: false,
    isRetryable: true,
  },
  WALLET_WRONG_NETWORK: {
    code: "WALLET_WRONG_NETWORK",
    title: "Wrong network",
    message: "Your wallet is on a different chain than PaxeerNetworkeer.",
    suggestedFix: "Switch your wallet to Paxeer Network (Chain ID 125).",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  WALLET_INSUFFICIENT_FUNDS: {
    code: "WALLET_INSUFFICIENT_FUNDS",
    title: "Not enough balance",
    message: "Your wallet doesn't have enough funds to cover this action plus gas.",
    suggestedFix: "Top up PAX or reduce the amount and try again.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  WALLET_PROVIDER_UNAVAILABLE: {
    code: "WALLET_PROVIDER_UNAVAILABLE",
    title: "Wallet unavailable",
    message: "We couldn't reach your wallet provider.",
    suggestedFix: "Unlock your wallet extension or refresh the page.",
    severity: "warning",
    isReportable: true,
    isRetryable: true,
  },
  WALLET_TIMEOUT: {
    code: "WALLET_TIMEOUT",
    title: "Wallet timed out",
    message: "The wallet didn't respond in time.",
    suggestedFix: "Open your wallet, then try again.",
    severity: "warning",
    isReportable: true,
    isRetryable: true,
  },

  // ── On-chain / RPC ────────────────────────────────────────────────
  RPC_DISCONNECTED: {
    code: "RPC_DISCONNECTED",
    title: "Network unreachable",
    message: "We can't reach the Paxeer Network right now.",
    suggestedFix: "Check your internet, then retry in a few seconds.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
  RPC_RATE_LIMITED: {
    code: "RPC_RATE_LIMITED",
    title: "Slowing down",
    message: "You're sending requests faster than the network allows.",
    suggestedFix: "Wait a moment and try again.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  RPC_REVERT: {
    code: "RPC_REVERT",
    title: "Transaction reverted",
    message: "The contract rejected this transaction.",
    suggestedFix: "Double-check the inputs — the action may not be allowed right now.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
  RPC_GAS_ESTIMATION_FAILED: {
    code: "RPC_GAS_ESTIMATION_FAILED",
    title: "Gas estimation failed",
    message: "We couldn't estimate gas — usually that means the transaction would fail on-chain.",
    suggestedFix: "Verify your inputs and that the position/order is still valid.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
  RPC_NONCE_TOO_LOW: {
    code: "RPC_NONCE_TOO_LOW",
    title: "Pending transaction conflict",
    message: "Your wallet has another transaction in flight at the same nonce.",
    suggestedFix: "Wait for the pending transaction to clear, or speed it up in your wallet.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  RPC_REPLACEMENT_UNDERPRICED: {
    code: "RPC_REPLACEMENT_UNDERPRICED",
    title: "Replacement gas too low",
    message: "Your replacement transaction's gas price is below the original.",
    suggestedFix: "Bump the gas price in your wallet and resubmit.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  RPC_TX_TIMEOUT: {
    code: "RPC_TX_TIMEOUT",
    title: "Transaction taking longer than usual",
    message: "Your transaction is still pending after a while.",
    suggestedFix: "Check your wallet — if it's stuck, speed up or cancel from there.",
    severity: "warning",
    isReportable: true,
    isRetryable: false,
  },

  // ── HTTP / API ────────────────────────────────────────────────────
  API_OFFLINE: {
    code: "API_OFFLINE",
    title: "You're offline",
    message: "We can't reach our servers from your connection.",
    suggestedFix: "Check your internet, then try again.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  API_TIMEOUT: {
    code: "API_TIMEOUT",
    title: "Request timed out",
    message: "The server didn't respond in time.",
    suggestedFix: "Try again — if it keeps happening, give us a few minutes.",
    severity: "warning",
    isReportable: true,
    isRetryable: true,
  },
  API_RATE_LIMITED: {
    code: "API_RATE_LIMITED",
    title: "Easy there",
    message: "You've hit the rate limit on this endpoint.",
    suggestedFix: "Wait a few seconds and try again.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  API_UNAUTHORIZED: {
    code: "API_UNAUTHORIZED",
    title: "Session expired",
    message: "Your sign-in session has expired or isn't valid for this action.",
    suggestedFix: "Reconnect your wallet to refresh your session.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  API_FORBIDDEN: {
    code: "API_FORBIDDEN",
    title: "Action not allowed",
    message: "Your account doesn't have permission for this.",
    suggestedFix: "If you think this is wrong, please report it.",
    severity: "warning",
    isReportable: true,
    isRetryable: false,
  },
  API_NOT_FOUND: {
    code: "API_NOT_FOUND",
    title: "Not found",
    message: "We couldn't find the data you asked for.",
    suggestedFix: "Refresh the page — it may have moved or been removed.",
    severity: "warning",
    isReportable: true,
    isRetryable: true,
  },
  API_BAD_REQUEST: {
    code: "API_BAD_REQUEST",
    title: "Invalid request",
    message: "Something about that request didn't look right to the server.",
    suggestedFix: "Refresh the page and try again.",
    severity: "warning",
    isReportable: true,
    isRetryable: true,
  },
  API_SERVER_ERROR: {
    code: "API_SERVER_ERROR",
    title: "Something went wrong on our end",
    message: "Our servers hit an error processing this. We've been notified.",
    suggestedFix: "Try again shortly.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
  API_BAD_GATEWAY: {
    code: "API_BAD_GATEWAY",
    title: "Backend unreachable",
    message: "An upstream service we depend on is currently unavailable.",
    suggestedFix: "Try again in a moment.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
  API_PAYLOAD_INVALID: {
    code: "API_PAYLOAD_INVALID",
    title: "Unexpected response",
    message: "The server returned data we didn't recognise.",
    suggestedFix: "Refresh the page — if the issue persists, please report it.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },

  // ── Live data / WebSocket ─────────────────────────────────────────
  WS_CONNECT_FAILED: {
    code: "WS_CONNECT_FAILED",
    title: "Live data offline",
    message: "We couldn't open a live connection.",
    suggestedFix: "We're auto-reconnecting — check your network if this persists.",
    severity: "warning",
    isReportable: true,
    isRetryable: true,
  },
  WS_DISCONNECTED: {
    code: "WS_DISCONNECTED",
    title: "Connection dropped",
    message: "The live data feed disconnected.",
    suggestedFix: "Reconnecting automatically — your screen will refresh shortly.",
    severity: "info",
    isReportable: false,
    isRetryable: true,
  },
  WS_STALE: {
    code: "WS_STALE",
    title: "Data may be out of date",
    message: "We haven't seen an update from the live feed in a while.",
    suggestedFix: "Refresh the page if quotes look stuck.",
    severity: "info",
    isReportable: false,
    isRetryable: true,
  },

  // ── Form / validation ─────────────────────────────────────────────
  VALIDATION_AMOUNT_INVALID: {
    code: "VALIDATION_AMOUNT_INVALID",
    title: "Invalid amount",
    message: "That amount isn't a valid number.",
    suggestedFix: "Enter a positive number with at most the allowed decimals.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  VALIDATION_INSUFFICIENT_BALANCE: {
    code: "VALIDATION_INSUFFICIENT_BALANCE",
    title: "Not enough balance",
    message: "You don't have enough of this asset.",
    suggestedFix: "Lower the amount or deposit more first.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  VALIDATION_BELOW_MIN: {
    code: "VALIDATION_BELOW_MIN",
    title: "Below minimum",
    message: "That's below the minimum allowed for this action.",
    suggestedFix: "Increase the amount above the minimum.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  VALIDATION_ABOVE_MAX: {
    code: "VALIDATION_ABOVE_MAX",
    title: "Above maximum",
    message: "That's above the maximum allowed for this action.",
    suggestedFix: "Reduce the amount below the maximum.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  VALIDATION_PRECISION: {
    code: "VALIDATION_PRECISION",
    title: "Too many decimals",
    message: "This token doesn't allow that many decimal places.",
    suggestedFix: "Round the amount to fewer decimals.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  VALIDATION_REQUIRED_FIELD: {
    code: "VALIDATION_REQUIRED_FIELD",
    title: "Missing field",
    message: "A required field hasn't been filled in.",
    suggestedFix: "Complete the highlighted field and try again.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },

  // ── Trade / position lifecycle ────────────────────────────────────
  TRADE_SLIPPAGE_EXCEEDED: {
    code: "TRADE_SLIPPAGE_EXCEEDED",
    title: "Slippage too high",
    message: "Price moved beyond your slippage tolerance before execution.",
    suggestedFix: "Increase your slippage tolerance or try again at a calmer moment.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  TRADE_MARKET_HALTED: {
    code: "TRADE_MARKET_HALTED",
    title: "Market paused",
    message: "Trading on this market is temporarily halted.",
    suggestedFix: "Try again once trading resumes — check the market status banner.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  TRADE_INSUFFICIENT_LIQUIDITY: {
    code: "TRADE_INSUFFICIENT_LIQUIDITY",
    title: "Not enough liquidity",
    message: "There isn't enough on-book size to fill that order at the price you wanted.",
    suggestedFix: "Reduce the size or use a more aggressive price.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  TRADE_POSITION_LIQUIDATABLE: {
    code: "TRADE_POSITION_LIQUIDATABLE",
    title: "Position underwater",
    message: "This action would leave the position below maintenance margin.",
    suggestedFix: "Add collateral or reduce position size first.",
    severity: "error",
    isReportable: false,
    isRetryable: true,
  },
  TRADE_TP_SL_INVALID: {
    code: "TRADE_TP_SL_INVALID",
    title: "Trigger price invalid",
    message: "The take-profit or stop-loss price doesn't make sense for this position.",
    suggestedFix: "Set TP above and SL below the entry for longs (and the reverse for shorts).",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },

  // ── App / runtime ─────────────────────────────────────────────────
  RUNTIME_ABORTED: {
    code: "RUNTIME_ABORTED",
    title: "Cancelled",
    message: "The request was cancelled before it finished.",
    suggestedFix: null, // benign, usually invisible
    severity: "info",
    isReportable: false,
    isRetryable: true,
  },
  RUNTIME_HYDRATION: {
    code: "RUNTIME_HYDRATION",
    title: "Page didn't load cleanly",
    message: "Something rendered differently between the server and your browser.",
    suggestedFix: "Refresh the page — that usually fixes it.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
  RUNTIME_CHUNK_LOAD: {
    code: "RUNTIME_CHUNK_LOAD",
    title: "Update available",
    message: "We just shipped a new version and your tab is on an older one.",
    suggestedFix: "Refresh to get the latest version.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  RUNTIME_PERMISSION_DENIED: {
    code: "RUNTIME_PERMISSION_DENIED",
    title: "Permission needed",
    message: "Your browser blocked access to something we needed (clipboard, storage, etc.).",
    suggestedFix: "Allow the permission in your browser settings and try again.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },
  RUNTIME_QUOTA_EXCEEDED: {
    code: "RUNTIME_QUOTA_EXCEEDED",
    title: "Storage full",
    message: "Your browser has run out of space for this site.",
    suggestedFix: "Clear site data for paxeer-network in your browser settings.",
    severity: "warning",
    isReportable: false,
    isRetryable: true,
  },

  // ── Catch-all ─────────────────────────────────────────────────────
  UNKNOWN: {
    code: "UNKNOWN",
    title: "Something went wrong",
    message: "We hit an unexpected error. We've logged it, but a quick report from you helps a lot.",
    suggestedFix: "Try again — if it keeps happening, send us a report.",
    severity: "error",
    isReportable: true,
    isRetryable: true,
  },
};

/**
 * Resolve a catalog entry by code. If `code` is missing or unknown,
 * returns the `UNKNOWN` entry — never throws.
 */
export function getCatalogEntry(code: ErrorCode | undefined | null): CatalogEntry {
  if (!code) return ERROR_CATALOG.UNKNOWN;
  return ERROR_CATALOG[code] ?? ERROR_CATALOG.UNKNOWN;
}
