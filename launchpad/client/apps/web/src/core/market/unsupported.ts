import type { UseQueryResult } from '@tanstack/react-query';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import { isTransientError } from '@/core/transient-retry';
import { userFacingMessage } from '@/core/user-facing-error';

export interface Unsupported { state: 'unsupported'; operation: string; status: number | null; message: string }
export type Loadable<T> = { state: 'loading' } | { state: 'ready'; data: T } | Unsupported | { state: 'error'; error: Error; retry(): void };

export const unsupportedStatuses: ReadonlySet<number> = new Set([404, 501, 503, 412]);

export function unsupported(operation: string, status: number | null, message: string): Unsupported {
  return { state: 'unsupported', operation, status, message };
}

export function isUnsupported(value: unknown): value is Unsupported {
  return typeof value === 'object' && value !== null && (value as { state?: unknown }).state === 'unsupported' && typeof (value as { operation?: unknown }).operation === 'string';
}

interface StatusCarrier { status?: unknown; code?: unknown }

/**
 * Map a real request failure onto an Unsupported marker. Only the failure
 * classes that mean "this backend path is not served here" qualify:
 * CapabilityUnavailableError (404 without an error envelope / 501) and
 * HttpError with status 404, 501, 503 or 412. Every other error returns null
 * so the caller keeps treating it as a genuine error.
 */
export function toUnsupported(operation: string, error: unknown): Unsupported | null {
  if (error instanceof CapabilityUnavailableError) return unsupported(operation, error.status, error.message);
  if (error instanceof HttpError) return unsupportedStatuses.has(error.status) ? unsupported(operation, error.status, error.envelope?.code ?? error.message) : null;
  if (typeof error === 'object' && error !== null && error instanceof Error) {
    const status = (error as StatusCarrier).status;
    if (typeof status === 'number' && unsupportedStatuses.has(status)) return unsupported(operation, status, typeof (error as StatusCarrier).code === 'string' ? String((error as StatusCarrier).code) : error.message);
  }
  return null;
}

/**
 * Adapt a React Query result to the Loadable discriminated union. While a
 * transient failure is being retried React Query keeps the query pending (or
 * successful with its data), so the caller stays on its skeleton or last-good
 * rows; a refetch whose transient retries ran out also keeps the last-good
 * data instead of replacing it with an error.
 */
export function loadable<T>(query: Pick<UseQueryResult<T>, 'status' | 'data' | 'error' | 'refetch'>, operation: string): Loadable<T> {
  if (query.status === 'pending') return { state: 'loading' };
  if (query.status === 'error' && query.data !== undefined && isTransientError(query.error)) return { state: 'ready', data: query.data };
  if (query.status === 'error') {
    const marker = toUnsupported(operation, query.error);
    if (marker) return marker;
    const error = query.error instanceof Error ? query.error : new Error(String(query.error));
    return { state: 'error', error, retry: () => { void query.refetch(); } };
  }
  return { state: 'ready', data: query.data as T };
}

/**
 * True when a failure only means "the viewer is not signed in": a 401/403 from
 * the service, or the SDK refusing to send an authenticated request without a
 * session. Widgets render the signed-out prompt for these, never an error.
 */
export function isSignedOutError(error: unknown): boolean {
  if (error instanceof HttpError || error instanceof CapabilityUnavailableError) return error.status === 401 || error.status === 403;
  if (error instanceof TypeError) return error.message === 'Session required' || error.message === 'Access token required';
  if (typeof error === 'object' && error !== null && error instanceof Error) {
    const status = (error as StatusCarrier).status;
    return status === 401 || status === 403;
  }
  return false;
}

/**
 * True when a failure says "no service answers this route here" rather than
 * "try again": a 501, a CapabilityUnavailableError, or a 503 whose error
 * envelope says `retryable: false` (core/api, the gateway and pnl-tracker
 * answer unserved development routes with `503 DEPENDENCY_UNAVAILABLE
 * {"retryable":false}` and no Retry-After). Reads the envelope from an SDK
 * HttpError (`envelope.retryable`) or from a transport error that carries
 * `retryable` itself (AccountApiError, CalloutReadError).
 */
export function isNotServedError(error: unknown): boolean {
  if (error instanceof CapabilityUnavailableError) return true;
  if (typeof error !== 'object' || error === null) return false;
  const carrier = error as { status?: unknown; retryable?: unknown; envelope?: unknown };
  if (carrier.status === 501) return true;
  if (carrier.status !== 503 || isTransientError(error)) return false;
  const envelope = typeof carrier.envelope === 'object' && carrier.envelope !== null ? carrier.envelope as { retryable?: unknown } : carrier;
  return envelope.retryable === false;
}

/**
 * Plain wording for a failed account action: "<title> is not available yet"
 * when the route is not served (isNotServedError), otherwise the usual
 * userFacingMessage. Never returns a raw code.
 */
export function actionFailureMessage(title: string, error: unknown): string {
  return isNotServedError(error) ? `${title} is not available yet` : userFacingMessage(error);
}
