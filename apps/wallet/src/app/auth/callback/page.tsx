/**
 * Server-component wrapper for the auth-callback client. Exists purely
 * to attach `dynamic = 'force-dynamic'` at the route segment level
 * without contaminating the client component file.
 *
 * The page itself is rendered by `CallbackClient`, which lives in the
 * sibling file. We mark this route fully dynamic because:
 *
 *   - The page reads URL fragment params written by Supabase after
 *     OAuth/magic-link redirect — there's nothing to pre-render.
 *   - Build-time SSG runs without Supabase env vars in many CI
 *     environments, leaving `EmbeddedWalletProvider` unmounted and
 *     causing embedded-wallet hooks to error during prerender.
 */

import CallbackClient from './CallbackClient';

export const dynamic = 'force-dynamic';

export default function AuthCallbackPage() {
  return <CallbackClient />;
}
