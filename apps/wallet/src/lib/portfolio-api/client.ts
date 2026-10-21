/**
 * Minimal JSON-over-fetch helper for the Portfolio API (v2 watch + webhooks).
 *
 * Replaces the auto-generated `@paxeer/portfolio-sdk` Client + 4 @apimatic
 * runtime packages. Reads `PAXEER_CONFIG.portfolioApiBase` lazily so a
 * server-side proxy could be wired in later without touching call sites.
 */

import { PAXEER_CONFIG } from '@/lib/constants';

export interface PortfolioApiOptions {
  signal?: AbortSignal;
}

const baseUrl = (): string => PAXEER_CONFIG.portfolioApiBase.replace(/\/+$/, '');

export class PortfolioApiError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(status: number, body: unknown, message?: string) {
    super(message ?? `Portfolio API error ${status}`);
    this.name = 'PortfolioApiError';
    this.status = status;
    this.body = body;
  }
}

const parseBody = async (res: Response): Promise<unknown> => {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
};

const request = async <T>(
  method: string,
  path: string,
  init: { body?: unknown; query?: Record<string, string | number | undefined>; signal?: AbortSignal } = {},
): Promise<T> => {
  const url = new URL(`${baseUrl()}${path.startsWith('/') ? path : `/${path}`}`, typeof window === 'undefined' ? undefined : window.location.origin);
  if (init.query) {
    for (const [key, value] of Object.entries(init.query)) {
      if (value !== undefined && value !== null) url.searchParams.set(key, String(value));
    }
  }

  const headers: Record<string, string> = { Accept: 'application/json' };
  let body: string | undefined;
  if (init.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(init.body);
  }

  const res = await fetch(url.toString(), { method, headers, body, signal: init.signal });
  const parsed = await parseBody(res);

  if (!res.ok) {
    throw new PortfolioApiError(res.status, parsed);
  }

  return parsed as T;
};

export const portfolioApi = {
  get:    <T>(path: string, query?: Record<string, string | number | undefined>, opts?: PortfolioApiOptions) =>
    request<T>('GET', path, { query, signal: opts?.signal }),
  post:   <T>(path: string, body?: unknown, opts?: PortfolioApiOptions) =>
    request<T>('POST', path, { body, signal: opts?.signal }),
  patch:  <T>(path: string, body?: unknown, opts?: PortfolioApiOptions) =>
    request<T>('PATCH', path, { body, signal: opts?.signal }),
  delete: <T>(path: string, opts?: PortfolioApiOptions) =>
    request<T>('DELETE', path, { signal: opts?.signal }),
};

/**
 * Map a camelCase TS object to snake_case wire format. Only one level deep —
 * webhook payloads don't nest. Undefined keys are dropped.
 */
export const toSnake = (obj: Record<string, unknown>): Record<string, unknown> => {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    if (value === undefined) continue;
    const snake = key.replace(/[A-Z]/g, (m) => `_${m.toLowerCase()}`);
    out[snake] = value;
  }
  return out;
};
