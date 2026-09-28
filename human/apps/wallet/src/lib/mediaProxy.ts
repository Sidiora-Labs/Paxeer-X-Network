/**
 * Media proxy helpers — rewrite upstream media URLs to same-origin proxy routes
 * so browsers never make cross-origin image requests that would be blocked by ORB
 * (Opaque Response Blocking / ERR_BLOCKED_BY_ORB).
 *
 * Proxy routes:
 *   /api/sidiora/logo/{filename}  →  token logo PNGs (Railway + sidiora.fun)
 */

import { safeMediaPath } from '@/lib/security/media-policy';

/**
 * Rewrite a token logo URL to the same-origin proxy path.
 * Returns the original URL unchanged if it doesn't match any known upstream.
 * Returns undefined for falsy input.
 */
export function rewriteLogoUrl(url: string | null | undefined): string | undefined {
  if (!url) return undefined;
  return safeMediaPath(url);
}
