// ── Self-custody facade ───────────────────────────────────────────────
export { PaxeerWallet } from './PaxeerWallet';
export type { PaxeerWalletDeps } from './PaxeerWallet';

// ── Hardened self-custody core modules ────────────────────────────────
export {
    AuthenticationManager,
    IndexedDBStorageAdapter,
    LegacyMigrationManager,
    SessionManager,
    TransactionServiceV2,
    VaultManager,
    VaultSigner,
    WalletCoreV2,
    WalletError,
    WebCryptoAdapter,
} from './v2';
export type { WalletErrorCode } from './v2';

// ── Embedded / Funded wallet (Paxeer-managed custody via Supabase + REST)
//
// Mirror of the self-custody facade for the second + third custody models
// the PaxPort PWA supports. Read `./embedded/README.md` (or the top-level
// README.md) for the integration guide.
export {
    EmbeddedWallet,
    EmbeddedSigner,
    EmbeddedWalletProvider,
    FundedWallet,
    FundedSigner,
    PaxeerWalletError,
    buildEmbeddedConfig,
    getAuthRedirectUrl,
    getEmbeddedWallet,
    resolveEmbeddedConfigFromEnv,
    useEmbeddedAvailability,
    useEmbeddedWallet,
    useOptionalEmbeddedWallet,
    usePaxeerWallet,
    useSession,
    useWallet,
    DEFAULT_API_URL as EMBEDDED_DEFAULT_API_URL,
    DEFAULT_CHAIN_ID as EMBEDDED_DEFAULT_CHAIN_ID,
    DEFAULT_RPC_URL as EMBEDDED_DEFAULT_RPC_URL,
    DEFAULT_SUPABASE_URL as EMBEDDED_DEFAULT_SUPABASE_URL,
} from './embedded';
export type {
    ChainInfo,
    EmbeddedSignerOptions,
    EmbeddedSignInProvider,
    EmbeddedWalletContextValue,
    EmbeddedWalletOptions,
    EmbeddedWalletProviderProps,
    FundedAccount,
    FundedAccountStatus,
    FundedBalances,
    FundedDenyCode,
    FundedDenyDetail,
    FundedProvisionResponse,
    FundedSelfResponse,
    FundedSignerOptions,
    FundedTier,
    FundedTierSummary,
    FundedWhitelistEntry,
    OAuthProvider,
    PaxeerEmbeddedConfig,
    PublicWallet,
    SendTxResponse,
    SignMessageResponse,
    SignTxResponse,
    TxRequest,
    UseWalletResult,
    UseSessionResult,
} from './embedded';

// ── Types ─────────────────────────────────────────────────────────────
export type {
    SelfCustodyWalletSnapshot,
    WalletAccount,
    TransactionData,
    PaxeerWalletConfig,
} from './types';
export { WalletEvents } from './types';

// ── Port interfaces (for custom adapter authors + polymorphic UI) ─────
export type { CryptoPort, EventPort, StoragePort, TimerPort } from './v2';
export type { IEventBus, EventHandler } from './ports/IEventBus';
export type { ISelfCustodyWallet, IWallet, WalletKind } from './ports/IWallet';

// ── Shared non-security event compatibility ───────────────────────────
export { SimpleEventBus } from './adapters/SimpleEventBus';
