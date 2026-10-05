# `@paxeer/wallet`

TypeScript SDK for the Paxeer X wallet gateway: Supabase email and OAuth sign-in, the embedded wallet, an EIP-1193 provider and clients for the human service and the LayerX kernel.

## Install and build

The package is a member of the `human/wallet` pnpm workspace and is not published to a registry; workspace packages depend on it as `"@paxeer/wallet": "workspace:*"`, and `human/apps/wallet` links it from this directory. Its entry points are `@paxeer/wallet`, `@paxeer/wallet/react` and `@paxeer/wallet/provider`.

```sh
cd human/wallet
pnpm install
pnpm --filter @paxeer/wallet build       # also builds agent/sdk/typescript first
pnpm --filter @paxeer/wallet test        # vitest
pnpm --filter @paxeer/wallet typecheck
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

`PaxeerWallet`:

- `signInWithEmail(email, redirectTo?)`, `signInWithOAuth(provider, redirectTo?)`, `signOut()`
- `getSession()`, `getUser()`, `onAuthStateChange(cb)`
- `getWallet()`: returns `{ wallet, chain }` and provisions the wallet on first call
- `getStandardSelf()`, `provisionStandardWallet()`
- `signTransaction(tx)`: returns the signed transaction; `sendTransaction(tx)`: signs and broadcasts, returns the hash
- `signMessage(message)`: EIP-191 `personal_sign`
- `reviewLxActivity`, `reviewLxSendAuthorization`, `approveLxActivity`, `signApprovedLxActivity`, `lxApprovalStatus`: LayerX kernel activity review, approval and signing
- `listFundedTiers()`, `getFundedSelf()`, `provisionFundedAccount()`, `signFundedTransaction()`, `sendFundedTransaction()`, `signFundedMessage()`

React hooks (`@paxeer/wallet/react`): `usePaxeerWallet`, `useSession`, `useWallet`, `useStandardAccount`, `useFundedTiers`.

Other exports: `PaxeerProvider` (EIP-1193), EIP-6963 `announceProvider`, `discoverProviders` and `install`, `HumanClient`, `KernelClient` and `KernelAvailability`, `EndpointClient`, and the agent request and binding signing helpers.
