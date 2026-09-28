import { ethers } from 'ethers';
import {
    announceProvider,
    type Eip1193Provider,
    type ProviderEvent,
    type ProviderListener,
} from '@paxeer/wallet';

export const INJECTED_KEY = ethers.keccak256(ethers.toUtf8Bytes('paxeer-wallet-app-injected'));
export const INJECTED_UUID = '3a9e7c52-8d41-4f6b-a0c3-5e2d9b8f1a47';
export const INJECTED_ICON = 'data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciLz4=';

export interface RpcCall {
    readonly method: string;
    readonly params: readonly unknown[];
}

export class TestInjectedProvider implements Eip1193Provider {
    readonly wallet = new ethers.Wallet(INJECTED_KEY);
    readonly calls: RpcCall[] = [];
    readonly sent: string[] = [];
    authorised = false;
    chainId: number;
    private readonly listeners = new Map<ProviderEvent, Set<ProviderListener>>();

    constructor(chainId = 125) {
        this.chainId = chainId;
    }

    async request(args: { method: string; params?: readonly unknown[] | object }): Promise<unknown> {
        const params = Array.isArray(args.params) ? (args.params as readonly unknown[]) : [];
        this.calls.push({ method: args.method, params });
        switch (args.method) {
            case 'eth_requestAccounts':
                this.authorised = true;
                return [this.wallet.address];
            case 'eth_accounts':
                return this.authorised ? [this.wallet.address] : [];
            case 'eth_chainId':
                return ethers.toQuantity(this.chainId);
            case 'wallet_switchEthereumChain': {
                const target = params[0] as { chainId: string };
                this.chainId = Number(BigInt(target.chainId));
                this.emit('chainChanged', target.chainId);
                return null;
            }
            case 'personal_sign': {
                this.requireAccount(params[1]);
                const raw = String(params[0]);
                return this.wallet.signMessage(ethers.isHexString(raw) ? ethers.getBytes(raw) : raw);
            }
            case 'eth_signTypedData_v4': {
                this.requireAccount(params[0]);
                const raw = params[1];
                const data = (typeof raw === 'string' ? JSON.parse(raw) : raw) as {
                    domain: ethers.TypedDataDomain;
                    types: Record<string, ethers.TypedDataField[]>;
                    message: Record<string, unknown>;
                };
                const types = { ...data.types };
                delete types.EIP712Domain;
                return this.wallet.signTypedData(data.domain, types, data.message);
            }
            case 'eth_sendTransaction': {
                const tx = params[0] as Record<string, string | undefined>;
                this.requireAccount(tx.from);
                const signed = await this.wallet.signTransaction({
                    type: 2,
                    to: tx.to,
                    value: tx.value === undefined ? 0n : BigInt(tx.value),
                    data: tx.data ?? '0x',
                    gasLimit: tx.gas === undefined ? 21000n : BigInt(tx.gas),
                    maxFeePerGas: 1_000_000_000n,
                    maxPriorityFeePerGas: 1_000_000_000n,
                    nonce: this.sent.length,
                    chainId: this.chainId,
                });
                this.sent.push(signed);
                return ethers.keccak256(signed);
            }
            default:
                throw Object.assign(new Error(`unsupported method ${args.method}`), { code: 4200 });
        }
    }

    on(event: ProviderEvent, listener: ProviderListener): this {
        const set = this.listeners.get(event) ?? new Set<ProviderListener>();
        set.add(listener);
        this.listeners.set(event, set);
        return this;
    }

    removeListener(event: ProviderEvent, listener: ProviderListener): this {
        this.listeners.get(event)?.delete(listener);
        return this;
    }

    emit(event: ProviderEvent, payload: unknown): void {
        for (const listener of this.listeners.get(event) ?? []) listener(payload);
    }

    private requireAccount(address: unknown): void {
        if (!this.authorised) throw Object.assign(new Error('unauthorised'), { code: 4100 });
        if (typeof address !== 'string' || address.toLowerCase() !== this.wallet.address.toLowerCase()) {
            throw Object.assign(new Error('unknown account'), { code: 4100 });
        }
    }
}

export function announceInjected(target: EventTarget, provider = new TestInjectedProvider()): {
    provider: TestInjectedProvider;
    stop: () => void;
} {
    const stop = announceProvider(
        {
            info: { uuid: INJECTED_UUID, name: 'Test Browser Wallet', icon: INJECTED_ICON, rdns: 'test.example.wallet' },
            provider,
        },
        target,
    );
    return { provider, stop };
}
