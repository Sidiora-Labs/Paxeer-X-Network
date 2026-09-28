import type { Signer } from 'ethers';
import type { IEventBus } from './IEventBus';
import type {
    SelfCustodyWalletSnapshot,
    TransactionData,
    WalletAccount,
} from '../types';

/**
 * Discriminator that distinguishes the three custody models the PaxPort
 * wallet library ships:
 *
 *   - `'self-custody'` — `PaxeerWallet` from `/paxport/wallet/PaxeerWallet.ts`.
 *     PIN + authenticated Web Crypto vault in IndexedDB. The user owns
 *     the keys; the browser/device remains the trust boundary.
 *
 *   - `'embedded'` — `EmbeddedWallet` from `/paxport/wallet/embedded/`.
 *     Email / OAuth sign-in via Supabase, server-side signing via
 *     connect.paxportwallet.com. Same wallet on every Paxeer app the user
 *     signs into. Custody is Paxeer-managed.
 *
 *   - `'funded'` — `FundedWallet` from `/paxport/wallet/embedded/`. Shares
 *     the embedded auth surface (Supabase sign-in) but signs through the
 *     funded policy engine (`POST /v1/funded/send`) which enforces a
 *     per-tier contract / selector whitelist, drawdown caps, and a
 *     no-withdrawal rule. UI hides Send / Receive / off-ramp in this mode.
 */
export type WalletKind = 'self-custody' | 'embedded' | 'funded';

/**
 * Minimum surface that both wallet kinds expose to the PWA UI layer.
 *
 * The PWA can render a single screen flow that drives either implementation
 * by depending on `IWallet` and switching the concrete instance based on
 * what the user picked at onboarding. Anything kind-specific (PIN entry,
 * OAuth provider buttons, mnemonic backup) lives behind the kind-specific
 * sub-interface (`ISelfCustodyWallet` / `IEmbeddedWallet`).
 */
export interface IWallet {
    readonly kind: WalletKind;

    /** True if the wallet is fully provisioned and unlocked / authenticated. */
    isReady(): Promise<boolean>;

    /**
     * True if the user has previously initialized this wallet on this device
     * (self-custody) or has an active session (embedded).
     *
     * UI uses this to decide whether to render onboarding vs. unlock vs.
     * connected states.
     */
    hasWallet(): Promise<boolean>;

    /** Active account address (checksummed `0x…`). Throws if not ready. */
    getReceiveAddress(): Promise<string>;

    /** Send a native PAX or ERC-20 transfer. Returns the tx hash. */
    send(tx: TransactionData): Promise<string>;

    /** Wipe local state. For embedded this signs out of Supabase. */
    reset(): Promise<void>;
}

/**
 * Complete application-facing self-custody contract.
 *
 * Implementations keep wallet-core managers private and return only public
 * account metadata or keyless signer capabilities.
 */
export interface ISelfCustodyWallet extends IWallet {
    readonly kind: 'self-custody';
    readonly events: IEventBus;

    getSnapshot(): Promise<SelfCustodyWalletSnapshot>;
    getSessionTimeRemaining(): number;
    isSessionValid(): boolean;

    createNewWallet(
        password: string,
        accountName?: string,
    ): Promise<{ mnemonic: string; account: WalletAccount }>;
    restoreFromMnemonic(
        password: string,
        mnemonic: string,
    ): Promise<WalletAccount[]>;
    migrateLegacy(legacyPin: string, newPassword: string): Promise<void>;
    migratePassphraseToPin(currentPassphrase: string, newPin: string): Promise<void>;

    unlock(password: string): Promise<boolean>;
    reauthenticate(password: string): Promise<void>;
    lock(): Promise<void>;

    getAccounts(): Promise<WalletAccount[]>;
    getActiveAccount(): Promise<WalletAccount | null>;
    setActiveAccount(address: string): Promise<void>;
    deriveNextAccount(name: string): Promise<WalletAccount>;
    renameAccount(address: string, name: string): Promise<void>;
    deleteAccount(address: string): Promise<void>;
    importPrivateKey(privateKey: string, name: string): Promise<WalletAccount>;
    exportMnemonic(): Promise<string>;
    exportPrivateKey(address: string): Promise<string>;
    getSigner(address: string): Signer;
}
