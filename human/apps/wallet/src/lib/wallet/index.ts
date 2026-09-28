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
    WalletAccount,
    TransactionData,
} from './types';
export { WalletEvents } from './types';

// ── Port interfaces ─────
export type { IEventBus, EventHandler } from './ports/IEventBus';
export type { IWallet, WalletKind } from './ports/IWallet';

// ── Shared non-security event compatibility ───────────────────────────
export { SimpleEventBus } from './adapters/SimpleEventBus';
