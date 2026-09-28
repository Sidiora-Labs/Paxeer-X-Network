# `@paxeer/wallet`

Drop-in JS/TS SDK for the Paxeer Universal Embedded Wallet. Email/social sign-in via Supabase, transparent server-side signing, no popups.

## Install

```bash
pnpm add @paxeer/wallet @supabase/supabase-js
```

## Usage (vanilla)

```ts
import { PaxeerWallet } from '@paxeer/wallet';

const paxeer = new PaxeerWallet({
  apiUrl: 'https://wallet.example',
  supabaseUrl: process.env.NEXT_PUBLIC_SUPABASE_URL!,
  supabaseAnonKey: process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY!,
});

// Sign in with email magic link
await paxeer.signInWithEmail('alice@example.com');
// ...user clicks the link, Supabase puts a session in localStorage...

// Get the wallet (auto-provisions on first call)
const { wallet } = await paxeer.getWallet();
console.log(wallet.address); // 0x...

// Send a transaction — no popup, no signature prompt
const { tx_hash } = await paxeer.sendTransaction({
  to: '0x1234...',
  value: 1_000_000_000_000_000n, // 0.001 PAX
});
```

## Usage (React)

```tsx
import { usePaxeerWallet, useSession, useWallet } from '@paxeer/wallet/react';

const config = {
  apiUrl: process.env.NEXT_PUBLIC_PAXEER_WALLET_API!,
  supabaseUrl: process.env.NEXT_PUBLIC_SUPABASE_URL!,
  supabaseAnonKey: process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY!,
};

export function App() {
  const paxeer = usePaxeerWallet(config);
  const { user, loading: sessionLoading } = useSession(paxeer);
  const { wallet } = useWallet(paxeer);

  if (sessionLoading) return <p>Loading…</p>;
  if (!user) return <button onClick={() => paxeer.signInWithOAuth('google')}>Sign in</button>;
  return (
    <div>
      <p>Signed in as {user.email}</p>
      <p>Wallet: {wallet?.address ?? '…'}</p>
      <button
        onClick={async () => {
          const { tx_hash } = await paxeer.sendTransaction({
            to: '0x...',
            value: '1000000000000000', // wei as decimal string
          });
          console.log('sent', tx_hash);
        }}
      >
        Send
      </button>
    </div>
  );
}
```

## API surface

- `signInWithEmail(email, redirectTo?)`
- `signInWithOAuth(provider, redirectTo?)`
- `signOut()`
- `getSession()`, `getUser()`, `onAuthStateChange(cb)`
- `getWallet()` — returns `{ wallet, chain }`, auto-provisions
- `signTransaction(tx)` — returns serialized signed tx
- `sendTransaction(tx)` — sign + broadcast, returns tx hash
- `signMessage(message)` — EIP-191 personal_sign
