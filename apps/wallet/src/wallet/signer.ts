import { ethers } from 'ethers';
import type { Hex, TypedDataPayload, WalletInterface, WalletTransaction } from '@paxeer/wallet';

export class WalletSignerError extends Error {
    constructor(
        readonly code: 'raw_signing_unsupported' | 'message_not_text' | 'invalid_field' | 'no_provider' | 'transaction_not_visible',
        message: string,
        readonly hash?: Hex,
    ) {
        super(message);
        this.name = 'WalletSignerError';
    }
}

const TRANSACTION_POLL_MS = 250;
const TRANSACTION_POLL_ATTEMPTS = 20;

export class WalletSigner extends ethers.AbstractSigner {
    constructor(
        readonly wallet: WalletInterface,
        provider: ethers.Provider | null,
    ) {
        super(provider);
    }

    async getAddress(): Promise<string> {
        const [account] = await this.wallet.accounts();
        if (!account) throw new WalletSignerError('invalid_field', 'the wallet exposed no account');
        return ethers.getAddress(account);
    }

    connect(provider: ethers.Provider | null): WalletSigner {
        return new WalletSigner(this.wallet, provider);
    }

    async signTransaction(): Promise<string> {
        throw new WalletSignerError(
            'raw_signing_unsupported',
            'the wallet signs and sends in one request; use sendTransaction',
        );
    }

    async signMessage(message: string | Uint8Array): Promise<string> {
        let text: string;
        if (typeof message === 'string') text = message;
        else {
            try {
                text = new TextDecoder('utf-8', { fatal: true }).decode(message);
            } catch {
                throw new WalletSignerError('message_not_text', 'the wallet signs UTF-8 text messages only');
            }
        }
        return this.wallet.signMessage(text);
    }

    async signTypedData(
        domain: ethers.TypedDataDomain,
        types: Record<string, ethers.TypedDataField[]>,
        value: Record<string, unknown>,
    ): Promise<string> {
        const payload = ethers.TypedDataEncoder.getPayload(domain, types, value) as unknown as TypedDataPayload;
        return this.wallet.signTypedData(payload);
    }

    async sendTransaction(tx: ethers.TransactionRequest): Promise<ethers.TransactionResponse> {
        const provider = this.provider;
        if (!provider) {
            throw new WalletSignerError('no_provider', 'a provider is required to track a sent transaction');
        }
        const from = await this.getAddress();
        if (tx.from !== undefined && tx.from !== null) {
            const declared = await ethers.resolveAddress(tx.from, this.provider);
            if (declared.toLowerCase() !== from.toLowerCase()) {
                throw new WalletSignerError('invalid_field', 'from is not the connected account');
            }
        }
        const request: WalletTransaction = {};
        const writable = request as { -readonly [K in keyof WalletTransaction]: WalletTransaction[K] };
        if (tx.to !== undefined && tx.to !== null) {
            writable.to = (await ethers.resolveAddress(tx.to, this.provider)) as Hex;
        }
        if (tx.data !== undefined && tx.data !== null) writable.data = ethers.hexlify(tx.data) as Hex;
        if (tx.value !== undefined && tx.value !== null) writable.value = ethers.getBigInt(tx.value, 'value');
        if (tx.gasLimit !== undefined && tx.gasLimit !== null) writable.gas = ethers.getBigInt(tx.gasLimit, 'gasLimit');
        if (tx.maxFeePerGas !== undefined && tx.maxFeePerGas !== null) {
            writable.maxFeePerGas = ethers.getBigInt(tx.maxFeePerGas, 'maxFeePerGas');
        }
        if (tx.maxPriorityFeePerGas !== undefined && tx.maxPriorityFeePerGas !== null) {
            writable.maxPriorityFeePerGas = ethers.getBigInt(tx.maxPriorityFeePerGas, 'maxPriorityFeePerGas');
        }
        if (tx.nonce !== undefined && tx.nonce !== null) writable.nonce = ethers.getNumber(tx.nonce, 'nonce');
        if (tx.chainId !== undefined && tx.chainId !== null) writable.chainId = ethers.getNumber(tx.chainId, 'chainId');
        const hash = await this.wallet.sendTransaction(request);
        return this.transactionResponse(provider, hash);
    }

    private async transactionResponse(provider: ethers.Provider, hash: Hex): Promise<ethers.TransactionResponse> {
        for (let attempt = 0; attempt < TRANSACTION_POLL_ATTEMPTS; attempt += 1) {
            const response = await provider.getTransaction(hash);
            if (response) return response;
            await new Promise((resolve) => setTimeout(resolve, TRANSACTION_POLL_MS));
        }
        throw new WalletSignerError(
            'transaction_not_visible',
            `transaction ${hash} was accepted but is not yet visible on the RPC base`,
            hash,
        );
    }
}
