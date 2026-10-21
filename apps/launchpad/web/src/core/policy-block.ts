/**
 * Tells a Content-Security-Policy block apart from a network outage.
 *
 * A fetch or WebSocket refused by the page's CSP fails exactly like a dropped
 * connection (a bare `TypeError: Failed to fetch`, which the SDK reports as
 * `NETWORK_INTERRUPTED`), so retrying it only keeps the viewer in a loading
 * state for nothing. The browser does expose the block as a
 * `securitypolicyviolation` event on the document; this module records those
 * for `connect-src` and answers whether a network-class failure is really a
 * policy block. A request to an origin the bundle's own resolver does not
 * allow (core/backend-origins.ts browserConnectOrigins()) is a policy block
 * too, before any event arrives.
 *
 * A policy block is a configuration fault, never transient: it is not retried
 * (core/transient-retry.ts) and reads as `plainCopy.configuration`
 * (core/user-facing-error.ts).
 */
import { browserConnectOrigins } from './backend-origins';

/** How long a recorded connect-src violation explains later network-class failures. */
export const policyViolationWindowMs = 30_000;

/** The parts of a SecurityPolicyViolationEvent this module reads. */
export interface PolicyViolation { blockedURI: string; effectiveDirective: string }

const violations = new Map<string, number>();

function originOf(url: string, base?: string): string | null {
  try { const origin = new URL(url, base).origin; return origin === 'null' ? null : origin; } catch { return null; }
}

/** Records a connect-src violation (any other directive is ignored). */
export function recordPolicyViolation(event: PolicyViolation, now: number = Date.now()): void {
  if (!/^connect-src\b/.test(event.effectiveDirective)) return;
  const origin = originOf(event.blockedURI);
  if (origin) violations.set(origin, now);
}

/** Forgets every recorded violation. */
export function clearPolicyViolations(): void { violations.clear(); }

let installedOn: EventTarget | null = null;

/** Listens for `securitypolicyviolation` on the document (idempotent; no-op on the server). */
export function installPolicyViolationMonitor(target: EventTarget | null = typeof document === 'undefined' ? null : document): void {
  if (!target || installedOn === target) return;
  target.addEventListener('securitypolicyviolation', event => recordPolicyViolation(event as unknown as PolicyViolation));
  installedOn = target;
}

installPolicyViolationMonitor();

/** True when the bundle's resolver allows the URL's origin (or it is the page's own origin). */
export function isConnectAllowed(url: string, pageOrigin: string | null = typeof location === 'undefined' ? null : location.origin): boolean {
  const origin = originOf(url, pageOrigin ?? undefined);
  if (!origin) return false;
  if (pageOrigin && (origin === pageOrigin || origin === pageOrigin.replace(/^http/, 'ws'))) return true;
  return browserConnectOrigins().includes(origin);
}

/**
 * The blocked origin when a network-class failure is explained by the policy:
 * the request URL (when the caller knows it) is outside the allowed origins,
 * or a connect-src violation of one of the bundle's own origins (the served
 * policy disagrees with the bundle) was recorded within policyViolationWindowMs.
 * Null means "treat it as a network failure".
 */
export function policyBlockedOrigin(url?: string | null, now: number = Date.now()): string | null {
  if (url && !isConnectAllowed(url)) return originOf(url, typeof location === 'undefined' ? undefined : location.origin);
  // Only origins this bundle contacts count: a blocked third-party beacon says nothing about our reads.
  const ours = new Set(browserConnectOrigins());
  let latest: [string, number] | null = null;
  for (const entry of violations) if (ours.has(entry[0]) && now - entry[1] <= policyViolationWindowMs && (!latest || entry[1] > latest[1])) latest = entry;
  return latest ? latest[0] : null;
}

/** A request the page's Content-Security-Policy refused: a configuration fault, not an outage. */
export class PolicyBlockedError extends Error {
  readonly code = 'POLICY_BLOCKED';
  readonly origin: string;
  constructor(origin: string) {
    super(`Content-Security-Policy refused a connection to ${origin}`);
    this.name = 'PolicyBlockedError';
    this.origin = origin;
  }
}
