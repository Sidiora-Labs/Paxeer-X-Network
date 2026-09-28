# `/paxport/wallet`

The PaxPort wallet library.

Two custody models live side-by-side:

| Module | Custody | Auth | Key material | Best for |
|---|---|---|---|---|
| `PaxeerWallet` (`./PaxeerWallet.ts` + `./v2/`) | **Self-custody** | Six-digit PIN with optional device authentication | BIP39 mnemonic and imported keys in an AES-256-GCM vault in IndexedDB | Power users, "not your keys not your coins" |
| `EmbeddedWallet` (`./embedded/`) | **Paxeer-managed** | Email magic link / Google / Apple / X / GitHub / Discord (Supabase) | Encrypted server-side at `connect.paxportwallet.com` | Mainstream onboarding, no-seed UX, same wallet across every Paxeer app |

Both implement the common `IWallet` interface (`./ports/IWallet.ts`) so PWA UI code can drive either model polymorphically.

---

## Installing into the PaxPort PWA

The PWA's `package.json` must include these runtime deps:

```jsonc
{
  "dependencies": {
    // already present for self-custody
    "ethers":            "^6.0.0",
    "@scure/bip39":      "^1.5.4",
    "@scure/bip32":      "^1.6.2",
    "crypto-js":         "^4.2.0", // read-only legacy migration only

    // required for the embedded wallet
    "@supabase/supabase-js": "^2.45.4",

    // peer dep for hooks
    "react": "^18 || ^19"
  }
}
```

Then add to `.env.local` (or wherever your bundler picks up env):

```bash
# Required — Supabase publishable key (safe in browsers)
NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY=

# Optional overrides — defaults shown
NEXT_PUBLIC_SUPABASE_URL=https://supabase.paxeer.app
NEXT_PUBLIC_PAXEER_WALLET_API=https://connect.paxportwallet.com
NEXT_PUBLIC_PAXEER_RPC_URL=https://public-mainnet.rpcpaxeer.online/evm

# Optional — only set if your auth-callback page lives on a different host
# Default is `${window.location.origin}/auth/callback`
# NEXT_PUBLIC_AUTH_REDIRECT_URL=https://wallet.paxeer.app/auth/callback
```

If `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` is unset, `getEmbeddedWallet()` returns `null` and `useEmbeddedAvailability()` returns `false`. Build the UI to gracefully fall back to self-custody-only in that case.

> **Vite users:** swap the `NEXT_PUBLIC_` prefix for `VITE_`. The env resolver in `embedded/client.ts` recognises both.

---

## Wiring the React tree

```tsx
// app/_app.tsx (or main.tsx for Vite)
import { EmbeddedWalletProvider } from '@paxport/wallet'; // or your relative path

export default function App({ children }: { children: React.ReactNode }) {
  return (
    <EmbeddedWalletProvider>
      {children}
    </EmbeddedWalletProvider>
  );
}
```

Then anywhere below it:

```tsx
import { useEmbeddedWallet, useEmbeddedAvailability } from '@paxport/wallet';

function ConnectScreen() {
  const embeddedAvailable = useEmbeddedAvailability();
  const embedded = useEmbeddedWallet();

  if (!embeddedAvailable) {
    // Fall back to self-custody onboarding flow only
    return <SelfCustodyOnboarding />;
  }

  if (embedded.isReady) {
    return <WalletPanel address={embedded.publicWallet!.address} />;
  }

  if (embedded.isAuthenticated) {
    return <p>Provisioning your Paxeer wallet…</p>;
  }

  return (
    <div>
      <h1>Sign in to PaxPort</h1>
      <button disabled={embedded.authBusy} onClick={() => embedded.signInWithOAuth('google')}>
        Continue with Google
      </button>
      <button disabled={embedded.authBusy} onClick={() => embedded.signInWithOAuth('twitter')}>
        Continue with X
      </button>
      <button disabled={embedded.authBusy} onClick={() => embedded.signInWithOAuth('github')}>
        Continue with GitHub
      </button>
      <EmailForm onSubmit={(email) => embedded.signInWithEmail(email)} />

      <hr />
      <a href="/onboarding/self-custody">Or create a self-custody wallet (PIN + seed phrase)</a>
    </div>
  );
}
```

---

## Auth callback page

Supabase redirects users back to your app after OAuth or magic-link sign-in. Add a single route at `/auth/callback`:

```tsx
// pages/auth/callback.tsx (Next.js pages) or app/auth/callback/page.tsx (App Router)
'use client';
import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation'; // or 'next/router' for pages
import { getEmbeddedWallet } from '@paxport/wallet';

export default function AuthCallback() {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const wallet = getEmbeddedWallet();
    if (!wallet) {
      setError('Embedded wallet is not configured.');
      return;
    }
    // SDK auto-consumes the URL params. We just wait for the session.
    const unsub = wallet.onAuthStateChange((_event, session) => {
      if (session) {
        unsub();
        router.replace('/');
      }
    });
    // Safety: bail out after 8s
    const t = setTimeout(() => {
      unsub();
      setError('Sign-in did not complete. Please try again.');
    }, 8000);
    return () => { clearTimeout(t); unsub(); };
  }, [router]);

  return error
    ? <p>Sign-in failed: {error}</p>
    : <p>Finishing sign-in…</p>;
}
```

Then in the **Supabase dashboard → Authentication → URL Configuration → Redirect URLs**, add every origin/path the callback runs on (localhost, preview, prod).

---

## Sending transactions

Both wallet kinds expose the same `send(tx)` signature, so screens like SendForm don't care which custody model is active:

```tsx
import type { IWallet, TransactionData } from '@paxport/wallet';

async function sendTransaction(wallet: IWallet, tx: TransactionData): Promise<string> {
  if (!await wallet.isReady()) {
    throw new Error('Wallet not ready');
  }
  return wallet.send(tx);
}
```

Behind the scenes:

- **Self-custody** signs locally with ethers, broadcasts to `rpcUrl`.
- **Embedded** posts to `POST /v1/wallet/send` on `connect.paxportwallet.com`. The API server orchestrates the nodes alignment and  decrypts the user's key, signs with viem, and uses priority lane broadcasts. Returns the tx hash. No popups, no prompts.

ERC-20 transfers work via `tx.tokenAddress` on both. The embedded path encodes the `transfer(address,uint256)` calldata client-side and submits a regular transaction to the token contract — no extra server endpoint needed.

---

## Picking the active wallet at runtime

The PWA stores the user's choice somewhere persistent (your existing settings store) and constructs the right facade at app start:

```ts
import {
  PaxeerWallet,
  EmbeddedWallet,
  getEmbeddedWallet,
  type IWallet,
  type WalletKind,
} from '@paxport/wallet';

function buildWallet(kind: WalletKind): IWallet {
  if (kind === 'embedded') {
    const w = getEmbeddedWallet();
    if (!w) throw new Error('Embedded wallet not configured');
    return w;
  }
  return new PaxeerWallet({
    rpcUrl: process.env.NEXT_PUBLIC_PAXEER_RPC_URL!,
  });
}
```

Since both classes implement `IWallet`, the rest of the app accepts either.

The production React provider types the self-custody instance as
`ISelfCustodyWallet` and uses only facade methods. It does not receive the
vault, session, storage, crypto, transaction service, or `WalletCoreV2`
instances. State is loaded as one coherent public snapshot:

```ts
const wallet = new PaxeerWallet({ rpcUrl });
const state = await wallet.getSnapshot();

await wallet.setActiveAccount(address);
await wallet.deriveNextAccount('Trading');
const signer = wallet.getSigner(address); // keyless VaultSigner

await wallet.reauthenticate(pin);
const recoveryPhrase = await wallet.exportMnemonic();
```

`getSnapshot()` returns only wallet existence, migration status, lock state,
session time remaining, public accounts, and the active account. Secret export
requires the explicit `reauthenticate()` step immediately before export.

---

## Module map

```
/paxport/wallet
├── PaxeerWallet.ts           ← production self-custody facade
├── v2/
│   ├── adapters/
│   │   ├── web-crypto-adapter.ts
│   │   ├── indexeddb-storage-adapter.ts
│   │   └── legacy-cryptojs-reader.ts  ← migration only
│   ├── core/
│   │   ├── vault-manager.ts
│   │   ├── session-manager.ts
│   │   ├── authentication-manager.ts
│   │   ├── wallet-core.ts
│   │   ├── vault-signer.ts
│   │   └── legacy-migration-manager.ts
│   ├── ports/                ← cryptographic and transactional contracts
│   └── types/                ← public metadata and private vault schemas
├── embedded/                 ← Paxeer Embedded Wallet (new)
│   ├── EmbeddedWallet.ts     ← high-level facade, IWallet impl
│   ├── client.ts             ← singleton, env resolver, redirect URL
│   ├── react.ts              ← <EmbeddedWalletProvider>, useEmbeddedWallet
│   ├── sdk/                  ← vendored @paxeer/wallet
│   │   ├── index.ts          ← PaxeerWalletClient (raw REST client)
│   │   ├── react.ts          ← low-level useSdkSession / useSdkWallet
│   │   └── types.ts
│   └── index.ts              ← barrel
├── ports/
│   ├── IWallet.ts            ← unified custody-model port
│   └── IEventBus.ts          ← UI compatibility events
├── adapters/
│   └── SimpleEventBus.ts
└── index.ts                  ← top-level barrel — import from here
```

---

## Custody decision tree (for product copy on the onboarding screen)

```
Should I use embedded or self-custody?
│
├── User wants the same wallet on every Paxeer app
│   without a seed phrase to back up                 → Embedded
│
├── User has < $X exposure and just wants to trade   → Embedded
│
├── User insists on owning the keys themselves       → Self-custody
│
└── Power user / institutional                       → Self-custody
                                                       (or both — they
                                                       can hold an
                                                       embedded wallet
                                                       AND a self-custody
                                                       wallet in the same
                                                       app)
```

The onboarding UI should clearly disclose that **embedded means Paxeer Network Nodes hold each a partr of the encrypted key in persistent storage aligigning on signature requests done to the api `connect.paxportwallet.com` where the nodes determine the keys that need to be used based on the mapped user credenials to wallet address and walletr address to encrypted key** — that's the whole point of the no-seed UX, and users deserve to know.

---

## Backend wallet API contract (read-only, for app authors)

| Method | Path | Auth | Body | Returns |
|---|---|---|---|---|
| GET | `/v1/wallet/me` | Bearer JWT | — | `{ wallet, chain }` |
| POST | `/v1/wallet/provision` | Bearer JWT | — | `{ wallet }` |
| POST | `/v1/wallet/sign` | Bearer JWT | `{ tx }` | `{ signed_tx, address, chain_id }` |
| POST | `/v1/wallet/send` | Bearer JWT | `{ tx }` | `{ tx_hash, address, chain_id }` |
| POST | `/v1/wallet/sign-message` | Bearer JWT | `{ message }` | `{ signature, address }` |


Policy caps (enforced server-side, current at the time of writing):
- per-tx: ~1,898 PAX (~$25k)
- per-user-per-day: ~75,930 PAX (~$1M)
- rate limit: 60 signing requests / minute / user

Anything above these caps is rejected with a 4xx; step-up auth is planned for v1.1.

---

## Vendoring policy

`embedded/sdk/` is a vendored copy of `/paxport/paxeer-embedded-wallet/packages/sdk/src/`. We vendor (rather than `file:` linking) because `/paxport/wallet` is source-only and has no workspace context.

Re-vendor whenever the upstream SDK ships a material change:

```bash
cp /paxport/paxeer-embedded-wallet/packages/sdk/src/types.ts \
   /paxport/wallet/embedded/sdk/types.ts

# (manual rename: the upstream SDK exports `PaxeerWallet` — we re-export
# it as `PaxeerWalletClient` here to avoid colliding with the self-custody
# `PaxeerWallet` facade. Keep that rename across re-vendors.)
```

The vendored copy adds:
- Renamed `PaxeerWallet` → `PaxeerWalletClient`
- Renamed `PaxeerWalletConfig` → `PaxeerEmbeddedConfig`
- `OAuthProvider` is now an exported type rather than an inline literal
- Hooks renamed `useSession` → `useSdkSession`, `useWallet` → `useSdkWallet`, `usePaxeerWallet` → `useSdkClient` to avoid colliding with the higher-level `useEmbeddedWallet` exported from `embedded/react.ts`

Otherwise the public surface is identical to upstream.
