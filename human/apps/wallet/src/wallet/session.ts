import { ethers } from 'ethers';
import {
    PaxeerProvider,
    WalletInterface,
    discoverProviders,
    type ConfirmRequest,
    type Eip6963ProviderDetail,
    type Eip6963ProviderInfo,
    type Hex,
    type ProviderDiscovery,
    type WalletTransaction,
} from '@paxeer/wallet';
import { PAXEER_CONFIG } from '@/lib/constants';
import type { WalletConfig } from './config';
import type { IdentitySession } from './identity';

export const EMBEDDED_WALLET_INFO: Eip6963ProviderInfo = {
    uuid: '6f1c2d8e-3b4a-4c5d-9e6f-7a8b9c0d1e2f',
    name: 'Paxeer X Wallet',
    icon: 'data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAzMiAzMiI+PHJlY3Qgd2lkdGg9IjMyIiBoZWlnaHQ9IjMyIiByeD0iOCIvPjwvc3ZnPg==',
    rdns: 'network.paxeer.wallet',
};

export interface EmbeddedWalletOptions {
    readonly fetch?: typeof fetch;
    readonly confirm?: (request: ConfirmRequest) => boolean | Promise<boolean>;
}

export function embeddedWallet(
    config: WalletConfig,
    identity: IdentitySession,
    options: EmbeddedWalletOptions = {},
): WalletInterface {
    const provider = new PaxeerProvider({
        gatewayUrl: config.gatewayUrl,
        rpcUrl: config.rpcUrl,
        token: identity.gatewayToken,
        chainId: config.chainId,
        confirm: options.confirm,
        fetch: options.fetch,
    });
    return new WalletInterface(provider, EMBEDDED_WALLET_INFO);
}

export function injectedWallet(detail: Eip6963ProviderDetail, admission?: { readonly chainId: number; readonly account: Hex; readonly current: () => boolean }): WalletInterface {
    return new WalletInterface(detail.provider, detail.info, admission);
}

export function isEmbeddedDetail(detail: Eip6963ProviderDetail): boolean {
    return detail.provider instanceof PaxeerProvider;
}

export function discoverInjectedWallets(
    onProvider: (detail: Eip6963ProviderDetail) => void,
    target?: EventTarget,
): ProviderDiscovery {
    return discoverProviders((detail) => {
        if (!isEmbeddedDetail(detail)) onProvider(detail);
    }, target);
}

export async function authorisedAccount(detail: Eip6963ProviderDetail): Promise<Hex | null> {
    const accounts = await detail.provider.request({ method: 'eth_accounts' });
    if (!Array.isArray(accounts)) return null;
    const first = accounts[0];
    return typeof first === 'string' && ethers.isAddress(first) ? (first as Hex) : null;
}

export interface TransferRequest {
    readonly to: string;
    readonly value: string;
    readonly tokenAddress?: string;
    readonly decimals?: number;
}

const ERC20 = new ethers.Interface(['function transfer(address to, uint256 amount) returns (bool)']);

export class TransferError extends Error {
    constructor(readonly field: keyof TransferRequest, message: string) {
        super(message);
        this.name = 'TransferError';
    }
}

export function transferTransaction(request: TransferRequest, chainId = PAXEER_CONFIG.chainId): WalletTransaction {
    if (!ethers.isAddress(request.to)) throw new TransferError('to', 'the recipient is not an address');
    const decimals = request.decimals ?? 18;
    if (!Number.isInteger(decimals) || decimals < 0 || decimals > 36) {
        throw new TransferError('decimals', 'token decimals are out of range');
    }
    let amount: bigint;
    try {
        amount = ethers.parseUnits(request.value, decimals);
    } catch {
        throw new TransferError('value', 'the amount is not a decimal number');
    }
    if (amount <= 0n) throw new TransferError('value', 'the amount must be greater than zero');
    const to = ethers.getAddress(request.to) as Hex;
    if (request.tokenAddress === undefined) return { to, value: amount, chainId };
    if (!ethers.isAddress(request.tokenAddress)) {
        throw new TransferError('tokenAddress', 'the token is not an address');
    }
    return {
        to: ethers.getAddress(request.tokenAddress) as Hex,
        value: 0n,
        chainId,
        data: ERC20.encodeFunctionData('transfer', [to, amount]) as Hex,
    };
}
