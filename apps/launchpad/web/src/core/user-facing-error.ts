/**
 * One place that turns transport codes, HTTP failures and SDK operation
 * markers into plain wording for people using the app.
 *
 * Raw codes (`NETWORK_INTERRUPTED`, `HTTP 503: DEPENDENCY_UNAVAILABLE`),
 * contract operation ids (`IF_POOL_STATS`), request paths
 * (`GET /v2/pools/{p}`), internal vocabulary ("integrity quarantined",
 * "upstream") and browser fetch messages never reach the screen. Technical
 * detail goes to `console.error` (`reportUserFacingError`) or stays on the
 * error's `cause`; it is never rendered, not even as an attribute.
 */

import { policyBlockedOrigin } from './policy-block';

export const plainCopy = {
  network: 'Connection problem, check your internet and try again',
  marketFeed: 'Prices are not loading right now, try again',
  notAvailable: 'Not available yet',
  signIn: 'Connect your wallet to see this',
  rateLimited: 'Too many requests, try again in a moment',
  server: 'Something went wrong, try again',
  timeout: 'Taking too long, try again',
  invalid: 'Something is not right, check it and try again',
  conflict: 'This just changed, refresh and try again',
  broadcastNotProvisioned: 'Going live is not available yet',
  stateConflict: 'This stream just changed, refresh to see what is live',
  sessionExpired: 'You were signed out, refresh to continue',
  upstreamUnavailable: 'Not loading right now, try again in a moment',
  configuration: 'Not loading right now, check back soon',
  generic: 'Something went wrong, try again',
} as const;

export type PlainCopyKey = keyof typeof plainCopy;

const networkCodes = new Set(['NETWORK_INTERRUPTED', 'NETWORK_ERROR', 'TRANSPORT_ERROR', 'STREAM_CLOSED', 'CONNECTION_LOST', 'FETCH_FAILED']);
const unavailableCodes = new Set(['CAPABILITY_UNAVAILABLE', 'NOT_FOUND', 'NOT_SERVED', 'NOT_IMPLEMENTED', 'POOL_NOT_FOUND', 'DEPENDENCY_UNAVAILABLE', 'PRECONDITION_FAILED']);
const authCodes = new Set(['UNAUTHORIZED', 'FORBIDDEN', 'SESSION_REQUIRED', 'AUTH_REQUIRED']);
const rateCodes = new Set(['RATE_LIMITED', 'TOO_MANY_REQUESTS']);
const timeoutCodes = new Set(['TIMEOUT', 'DEADLINE_EXCEEDED', 'ABORTED', 'REQUEST_TIMEOUT']);
const invalidCodes = new Set(['INVALID_INPUT', 'INVALID_REQUEST', 'VALIDATION_FAILED', 'BAD_REQUEST', 'CONTRACT_VALIDATION', 'FIELD_INVALID']);
const conflictCodes = new Set(['CONFLICT', 'STALE_REVISION', 'REVISION_MISMATCH', 'REVISION_CONFLICT']);

const networkMessages = /fetch failed|failed to fetch|load failed|networkerror|network request failed|illegal invocation|\btypeerror\b|\berr_[a-z_]+\b|\beconn(?:reset|refused|aborted)\b|\benotfound\b|\betimedout\b|socket hang up/i;
const timeoutMessages = /timed? ?out|aborted|signal is aborted/i;

/**
 * Internal vocabulary that marks a message as written for engineers even when
 * it reads like a sentence ("Ranking integrity quarantined", "Statistics
 * identity disagreement", "Approved network input is unavailable").
 */
const technicalVocabulary = /\b(?:attestation|canonical|epoch|fence|lease|upstream|projection|readiness|runtime|integrity|degradation|snapshot|frontier|budget|manifest|digest|sha-?256|cursor|partition|consumer|publication|outbox|relay|handoff|replay|unavailable|quarantine[d]?|disagreement|invariant|envelope|schema|watermark|sequence|revision|idempotency|correlation|nonce|payload|endpoint|indexer|gateway|bff|sdk|udf|rpc|api|http|json|websocket|stub|mock|fixture|debug|staging|null|undefined|conflict|mismatch|malformed|protocol|frame|acknowledgement|shape|bound)\b|not configured|not served|\bdev(?:elopment)? (?:mode|build|only)\b|\b(?:request|response|verification|connection) failed\b|\binvalid\b/i;

/** Classify an HTTP status into one of the plain messages; null for statuses with no fixed wording. */
export function copyKeyForStatus(status: number | null | undefined): PlainCopyKey | null {
  if (status === null || status === undefined || status === 0) return null;
  if (status === 401 || status === 403) return 'signIn';
  if (status === 408) return 'timeout';
  if (status === 429) return 'rateLimited';
  if (status === 400 || status === 422) return 'invalid';
  if (status === 409) return 'conflict';
  if (status === 404 || status === 410 || status === 412 || status === 501 || status === 503) return 'notAvailable';
  if (status >= 500) return 'server';
  return null;
}

/** Classify a transport or envelope code; null when the code is unknown. */
export function copyKeyForCode(code: string | null | undefined): PlainCopyKey | null {
  if (!code) return null;
  const normal = code.trim().toUpperCase();
  if (networkCodes.has(normal)) return 'network';
  if (unavailableCodes.has(normal)) return 'notAvailable';
  if (authCodes.has(normal)) return 'signIn';
  if (rateCodes.has(normal)) return 'rateLimited';
  if (timeoutCodes.has(normal)) return 'timeout';
  if (invalidCodes.has(normal)) return 'invalid';
  if (normal === 'BROADCAST_NOT_PROVISIONED') return 'broadcastNotProvisioned';
  if (normal === 'STATE_CONFLICT') return 'stateConflict';
  if (normal === 'AUTH_EXPIRED') return 'sessionExpired';
  if (normal === 'UPSTREAM_UNAVAILABLE') return 'upstreamUnavailable';
  if (normal === 'POLICY_BLOCKED') return 'configuration';
  if (conflictCodes.has(normal)) return 'conflict';
  return null;
}

/**
 * True when a string is machine text rather than a sentence written for
 * people: a bare code, an `HTTP 503` prefix, an operation id, a request path,
 * a path template or a browser fetch failure.
 */
export function isRawErrorText(text: string): boolean {
  const value = text.trim();
  if (!value) return true;
  if (/^[A-Z][A-Z0-9_.:-]{2,}$/.test(value)) return true;
  if (/^[a-z]+(?:_[a-z0-9]+)+$/.test(value)) return true;
  if (/^HTTP \d{3}\b/.test(value)) return true;
  if (/\bIF_[A-Z0-9_]+/.test(value)) return true;
  if (/\b(?:GET|POST|PUT|PATCH|DELETE)\s+\//.test(value)) return true;
  if (/\/(?:v\d|bff|api)\/|\{[a-zA-Z]+\}/.test(value)) return true;
  if (/\b[A-Z]{2,}(?:_[A-Z0-9]+)+\b/.test(value)) return true;
  if (networkMessages.test(value)) return true;
  if (/^(?:Capability unavailable|Session required|Access token required|Invalid query|Unexpected token|JSON\.parse)/i.test(value)) return true;
  if (technicalVocabulary.test(value)) return true;
  // Wallet and contract-library errors (multi-line, long, carrying hex data or call dumps) are written for engineers.
  if (/\n/.test(value) || value.length > 160) return true;
  if (/\b0x[0-9a-f]{8,}|viem@|Request Arguments|Contract Call|reverted with|execution reverted/i.test(value)) return true;
  return false;
}

function keyForRawText(text: string): PlainCopyKey {
  const value = text.trim();
  const http = /^HTTP (\d{3})(?::\s*([A-Z0-9_]+))?/.exec(value);
  if (http) return copyKeyForCode(http[2]) ?? copyKeyForStatus(Number(http[1])) ?? 'generic';
  const coded = copyKeyForCode(value) ?? copyKeyForCode(/\b([A-Z]{2,}(?:_[A-Z0-9]+)+)\b/.exec(value)?.[1]);
  if (coded) return coded;
  if (/^[a-z]+(?:_[a-z0-9]+)+$/.test(value)) return copyKeyForCode(value) ?? 'generic';
  if (networkMessages.test(value)) return 'network';
  if (timeoutMessages.test(value)) return 'timeout';
  if (/^(?:Session required|Access token required)/i.test(value)) return 'signIn';
  if (/^Capability unavailable/i.test(value) || /\bIF_|\/(?:v\d|bff)\//.test(value)) return 'notAvailable';
  return 'generic';
}

/**
 * Plain wording for a string that may be a code. Sentences written for
 * people pass through; machine text maps to the matching plain message, or to
 * `fallback` when nothing more specific applies.
 */
export function plainText(text: string | null | undefined, fallback: string = plainCopy.generic): string {
  if (typeof text !== 'string' || !text.trim()) return fallback;
  if (!isRawErrorText(text)) return text;
  const key = keyForRawText(text);
  return key === 'generic' ? fallback : plainCopy[key];
}

interface ErrorCarrier { status?: unknown; code?: unknown; message?: unknown; name?: unknown }

/** HTTP status carried by an SDK or transport error, when there is one. */
export function errorStatus(error: unknown): number | null {
  if (typeof error !== 'object' || error === null) return null;
  const status = (error as ErrorCarrier).status;
  return typeof status === 'number' && Number.isFinite(status) ? status : null;
}

/** True when the failure is a definite "this record does not exist" (HTTP 404 or a not-found code). */
export function isNotFoundError(error: unknown): boolean {
  if (errorStatus(error) === 404) return true;
  const code = typeof error === 'object' && error !== null ? (error as ErrorCarrier).code : undefined;
  return typeof code === 'string' && /NOT_FOUND$/i.test(code);
}

/**
 * Plain wording for any thrown value. Codes and statuses on SDK errors are
 * checked before the message, so `StreamTransportError('NETWORK_INTERRUPTED')`
 * and `HttpError(503)` both read as sentences.
 */
export function userFacingMessage(error: unknown, fallback: string = plainCopy.generic): string {
  if (error instanceof UserFacingError) return error.userMessage;
  if (typeof error === 'string') return plainText(error, fallback);
  if (typeof error !== 'object' || error === null) return fallback;
  const carrier = error as ErrorCarrier;
  if (carrier.name === 'AbortError' || carrier.name === 'TimeoutError') return plainCopy.timeout;
  const code = typeof carrier.code === 'string' ? carrier.code : null;
  // The SDK labels any HttpError without an envelope TRANSPORT_ERROR, so a real status outranks that generic code.
  const generic = code === null || code.toUpperCase() === 'TRANSPORT_ERROR';
  const byCode = generic ? null : copyKeyForCode(code);
  // A connection the page's CSP refused fails like an outage; it is a configuration fault (core/policy-block.ts).
  if (byCode === 'network' && policyBlockedOrigin() !== null) return plainCopy.configuration;
  if (byCode) return plainCopy[byCode];
  if (carrier.name === 'TypeError' && typeof carrier.message === 'string' && networkMessages.test(carrier.message) && policyBlockedOrigin() !== null) return plainCopy.configuration;
  const byStatus = copyKeyForStatus(errorStatus(error));
  if (byStatus) return plainCopy[byStatus];
  if (code !== null && copyKeyForCode(code)) return plainCopy[copyKeyForCode(code)!];
  const message = typeof carrier.message === 'string' ? carrier.message : null;
  return plainText(message, fallback);
}

/**
 * An error whose message is already written for people. Hooks and clients
 * throw it (or store it) so screens can show `message` as is; the original
 * failure stays on `cause` for logs.
 */
export class UserFacingError extends Error {
  readonly userMessage: string;
  constructor(userMessage: string, cause?: unknown) {
    super(userMessage, cause === undefined ? undefined : { cause });
    this.name = 'UserFacingError';
    this.userMessage = userMessage;
  }
}

/**
 * Logs the technical failure to the console and returns the plain sentence to
 * show. Use it where a hook turns a caught error into screen state.
 */
export function reportUserFacingError(error: unknown, fallback: string = plainCopy.generic, context = 'request'): string {
  const message = userFacingMessage(error, fallback);
  if (!(error instanceof UserFacingError)) console.error(`[${context}]`, error);
  return message;
}

/** Wraps any thrown value as a `UserFacingError` with plain wording; the original stays on `cause`. */
export function toUserFacingError(error: unknown, fallback: string = plainCopy.generic): UserFacingError {
  if (error instanceof UserFacingError) return error;
  return new UserFacingError(userFacingMessage(error, fallback), error);
}

/**
 * Developer detail is never rendered: production is the only build. Kept as a
 * function so existing callers compile; always false.
 */
export function showsTechnicalDetail(_env?: string): boolean {
  return false;
}

/** Never rendered; always null. Technical detail belongs in `console.error`. */
export function technicalDetail(_operation: string | null | undefined, _status: number | null | undefined, _env?: string): string | null {
  return null;
}

/** Never rendered; always null. Technical detail belongs in `console.error`. */
export function errorTechnicalDetail(_error: unknown, _env?: string): string | null {
  return null;
}
