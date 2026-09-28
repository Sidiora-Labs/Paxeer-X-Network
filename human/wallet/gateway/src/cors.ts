/**
 * Compile a CORS origin entry into either an exact-match string or a RegExp
 * for wildcard subdomain matches.
 *
 *   "https://app.wallet.example"   -> "https://app.wallet.example" (exact)
 *   "https://*.wallet.example"     -> /^https:\/\/[^./]+\.wallet\.example$/
 *   "https://*.wallet.example:3000"-> /^https:\/\/[^./]+\.wallet\.example:3000$/
 *
 * The wildcard `*` matches a *single* subdomain level (`[^./]+`). This is the
 * industry convention (CloudFlare, Vercel, etc.) and prevents
 * `evil.foo.wallet.example` from matching `*.wallet.example`.
 *
 * For multi-level wildcard (rarely needed, e.g. preview deploys), set both:
 *   https://*.wallet.example,https://*.preview.wallet.example
 *
 * Kept in its own module so the wildcard logic can be unit-tested without
 * triggering `env.ts`'s top-level environment validation.
 */
export function compileOrigin(raw: string): string | RegExp {
  if (!raw.includes('*')) return raw;
  const escaped = raw
    // Escape every regex metachar except `*`, which we replace next.
    .replace(/[.+?^${}()|[\]\\/]/g, '\\$&')
    // `*` -> single subdomain level (no `.` or `/`).
    .replace(/\*/g, '[^./]+');
  return new RegExp(`^${escaped}$`);
}
