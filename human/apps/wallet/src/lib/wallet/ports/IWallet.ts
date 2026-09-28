import type { TransactionData } from '../types';

export type WalletKind = 'embedded' | 'injected';

/**
 * Minimum surface that both wallet kinds expose to the PWA UI layer.
 *
 * The PWA can render a single screen flow that drives either implementation
 * by depending on `IWallet` and switching the concrete instance based on
 * what the user picked at onboarding.
 */
export interface IWallet {
    readonly kind: WalletKind;

    /** True if the wallet is fully provisioned and authenticated. */
    isReady(): Promise<boolean>;

    /** True if the user has an active session with a provisioned account. */
    hasWallet(): Promise<boolean>;

    /** Active account address (checksummed `0x…`). Throws if not ready. */
    getReceiveAddress(): Promise<string>;

    /** Send a native PAX or ERC-20 transfer. Returns the tx hash. */
    send(tx: TransactionData): Promise<string>;

    /** Wipe local state. For embedded this signs out of Supabase. */
    reset(): Promise<void>;
}
