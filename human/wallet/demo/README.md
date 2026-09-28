# `@paxeer/demo` — Embedded Wallet reference app

Live demo of the Paxeer Embedded Wallet, **and** the canonical drop-in for any Paxeer Next.js app. Everything in `src/components/` is designed to be copy-pasted verbatim into your project.

Built on **Paxeer Brand Identity System v2.0 · 2026** — monochrome surfaces, Paxeer Blue (`#004CED`) reserved for primary actions, Inter for UI, JetBrains Mono for data, single easing curve.

---

## Run it locally

```bash
# 1. From the repo root, install workspace deps (if you haven't)
pnpm install

# 2. Configure the demo
#    create demo/.env.local with the NEXT_PUBLIC_* variables listed in deploy/env
# edit if you want to point at localhost:8787 instead of production

# 3. Make sure the wallet API container is up (production)
#    OR run it locally:  pnpm --filter @paxeer/api dev

# 4. Start the demo
pnpm --filter @paxeer/demo dev
```

Open http://localhost:3000.

> Important: in the **Supabase dashboard → Authentication → URL Configuration**, add `http://localhost:3000/auth/callback` (and your production demo URL if you deploy it) to the allowed redirect URLs. The OAuth providers (Google / X / GitHub) need this to round-trip back to your app.

---

## Drop the wallet into a partner Paxeer Next.js app

Three files copy verbatim (path-aware), one env block, one provider list:

```bash
# 1. install
pnpm add @paxeer/wallet @supabase/supabase-js @radix-ui/react-dialog viem

# 2. copy these from demo/src into your app
src/lib/paxeer.ts
src/lib/format.ts
src/components/ConnectButton.tsx
src/components/WalletModal.tsx
src/components/SendTxForm.tsx
src/components/ProviderButton.tsx
src/app/auth/callback/page.tsx
public/icons/{google,x,github,email}.svg
```

```bash
# 3. .env.local
NEXT_PUBLIC_PAXEER_WALLET_API=<wallet API base URL>
NEXT_PUBLIC_SUPABASE_URL=<Supabase project URL>
NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY=<Supabase publishable key>
NEXT_PUBLIC_AUTH_REDIRECT_URL=<your app origin>/auth/callback
```

```tsx
// 4. anywhere in your UI
import { ConnectButton } from '@/components/ConnectButton';

export function Header() {
  return (
    <header>
      <Logo />
      <ConnectButton />
    </header>
  );
}
```

That's it. The user gets the same wallet on your app as on every other Paxeer app, automatically.

---

## What the components do

| File | Role |
|---|---|
| `lib/paxeer.ts` | SDK singleton + `AUTH_REDIRECT_URL`. Reads env, throws loudly on misconfig. |
| `lib/format.ts` | `truncateAddress`, `weiToNative`, `nativeToWei`, `isAddress`. Pure utilities. |
| `components/ConnectButton.tsx` | The only thing your app needs to render. Two visual states (CTA / pill), opens the modal. |
| `components/WalletModal.tsx` | State machine: auth → wallet → send. Uses Radix Dialog under the hood. |
| `components/ProviderButton.tsx` | One row in the OAuth provider stack. Reusable. |
| `components/SendTxForm.tsx` | The send-tx form rendered inside the modal. |
| `app/auth/callback/page.tsx` | Where Supabase redirects users back to after OAuth. Restores session, returns to `/`. |

---

## Smoke-test checklist

After `pnpm dev`, verify each path:

- [ ] Click "Connect Paxeer" — modal opens with 3 OAuth buttons + email field
- [ ] Enter an email → "Send magic link" → "Check your email" state appears
- [ ] Click the magic link in your inbox → returns to `/auth/callback` → redirects to `/`
- [ ] ConnectButton swaps to the connected pill showing your truncated address
- [ ] Click the pill → modal opens to wallet view, shows full address (font-mono)
- [ ] Copy button works (turns green check for ~1.5s)
- [ ] Click "Send" → form opens, address validation works (red error on bad 0x)
- [ ] Enter a valid 0x address + amount → "Send Transaction" → spinner → success
- [ ] Click "View on explorer" — opens HyperPaxeer explorer in new tab
- [ ] Click "Sign out" → returns to auth view, ConnectButton swaps back to CTA
- [ ] Try Google / X / GitHub OAuth — each redirects, returns, lands you on the wallet view
- [ ] Open in another Paxeer app subdomain (when one exists) → same address persists

---

## Brand compliance (v2.0 · 2026)

This is the reference implementation. If you change anything, keep these rules:

- **Backgrounds** — `#050505` (black) or `#0B0B0B` (surface 800). No gradients.
- **Borders** — `#1E1E1E` (700). Subtle, never bold.
- **Text** — white on dark; muted text uses `#828282` (500) or `#AFAFAF` (400).
- **Primary action only** — Paxeer Blue `#004CED` (hover `#0040C9`). Used at most twice per surface.
- **Data is mono** — addresses, hashes, chain IDs use `var(--font-mono)` (JetBrains Mono).
- **Body is Inter** — all UI text. All weights are 400 except subtle emphasis at 500.
- **Motion** — single easing `cubic-bezier(0.4, 0, 0.2, 1)`. Hover/press = 150ms. Modal = 240ms.
- **No legacy cyan** — `#00E0FF` is deprecated. Anything that looks like that needs to be Paxeer Blue or a neutral.
