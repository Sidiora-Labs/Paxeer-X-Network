import { fetchWithPolicy as fetch } from "@/core/net/fetch";
/**
 * Low-level Paxscan HTTP fetcher.
 *
 * Zero dependencies beyond the platform `fetch`. No cookies, no CSRF, no auth,
 * no Rollbar/Mixpanel — the Paxscan (Blockscout) API is fully public.
 *
 * Replaces Blockscout's `lib/api/useApiFetch.tsx` with a plain function so the
 * client is framework-agnostic at the transport layer; hooks add react-query on top.
 */

export class PaxscanError extends Error {
  constructor(
    public readonly status: number,
    public readonly statusText: string,
    public readonly payload?: unknown,
  ) {
    super(`Paxscan ${ status } ${ statusText }`);
    this.name = 'PaxscanError';
  }
}

export async function paxscanFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);

  if (!res.ok) {
    let payload: unknown;
    try {
      payload = await res.json();
    } catch {
      // Response body is not JSON; leave payload undefined.
    }
    throw new PaxscanError(res.status, res.statusText, payload);
  }

  return res.json() as Promise<T>;
}
