/**
 * `/paxport/wallet/embedded` — public surface for the **Paxeer Embedded
 * Wallet** integration.
 *
 * Sibling to `/paxport/wallet/core/` (self-custody) and `/paxport/wallet/
 * PaxeerWallet.ts` (self-custody facade). PWA frontend devs typically need
 * three things from this module:
 *
 *   - `EmbeddedWalletProvider` + `useEmbeddedWallet` — drop into the React
 *     tree to expose session state, sign-in actions, and the provisioned
 *     wallet record to any component.
 *
 *   - `EmbeddedWallet` — non-React facade for background tasks, scripts,
 *     or a non-React shell. Implements `IWallet` so the same UI code can
 *     drive either custody model.
 *
 *   - `getEmbeddedWallet()` / `getAuthRedirectUrl()` — singleton + helper
 *     for the auth-callback page that handles Supabase OAuth returns.
 *
 * The vendored SDK (`PaxeerWalletClient`) is also re-exported as the
 * escape hatch for advanced flows.
 */

// ── Facades ──────────────────────────────────────────────────────────
export { EmbeddedWallet } from './EmbeddedWallet';
export { FundedWallet } from './FundedWallet';

// ── Signer adapters (let the swap SDK and other ethers.Contract
//    callers drive embedded / funded wallets without changes) ──
export { EmbeddedSigner } from './EmbeddedSigner';
export type { EmbeddedSignerOptions } from './EmbeddedSigner';
export { FundedSigner } from './FundedSigner';
export type { FundedSignerOptions } from './FundedSigner';

// ── Singleton + config ───────────────────────────────────────────────
export {
    buildEmbeddedConfig,
    getAuthRedirectUrl,
    getEmbeddedWallet,
    resolveEmbeddedConfigFromEnv,
    __resetEmbeddedWalletSingleton,
    DEFAULT_API_URL,
    DEFAULT_CHAIN_ID,
    DEFAULT_RPC_URL,
    DEFAULT_SUPABASE_URL,
} from './client';
export type { EmbeddedWalletOptions } from './client';

// ── React ────────────────────────────────────────────────────────────
export {
    EmbeddedWalletProvider,
    useEmbeddedAvailability,
    useEmbeddedWallet,
    useOptionalEmbeddedWallet,
} from './react';
export type {
    EmbeddedSignInProvider,
    EmbeddedWalletContextValue,
    EmbeddedWalletProviderProps,
} from './react';

// ── SDK escape hatch ─────────────────────────────────────────────────
export { PaxeerWallet, PaxeerWalletError } from './sdk';
export type {
    ChainInfo,
    OAuthProvider,
    PaxeerEmbeddedConfig,
    PublicWallet,
    SendTxResponse,
    SignMessageResponse,
    SignTxResponse,
    TxRequest,
    // Funded account types — used by the funded-mode UI surfaces (status
    // card, tier picker, settings panel, deny modal).
    FundedAccount,
    FundedAccountStatus,
    FundedBalances,
    FundedDenyCode,
    FundedDenyDetail,
    FundedProvisionResponse,
    FundedSelfResponse,
    FundedTier,
    FundedTierSummary,
    FundedWhitelistEntry,
} from './sdk/types';

// Low-level SDK hooks — only exposed for power users who want to drive the
// raw client outside the EmbeddedWalletProvider tree.
export { usePaxeerWallet, useSession, useWallet } from './sdk/react';
export type { UseSessionResult, UseWalletResult } from './sdk/react';
