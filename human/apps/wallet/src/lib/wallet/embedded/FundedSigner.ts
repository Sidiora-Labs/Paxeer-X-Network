/**
 * `FundedSigner` — an `ethers.Signer` that delegates transaction signing
 * **and** broadcast to the Paxeer Funded Wallet API
 * (`POST /v1/funded/send` on `connect.paxportwallet.com`).
 *
 * Sibling to `EmbeddedSigner` — same surface, same wait() semantics, but
 * every `sendTransaction()` runs through the funded policy engine first:
 *
 *   - Tx target must be in the user's tier whitelist (otherwise
 *     `CONTRACT_NOT_WHITELISTED`).
 *   - Approve spender must be whitelisted (`APPROVE_SPENDER_NOT_WHITELISTED`).
 *   - Native value gated per whitelist entry (`NATIVE_VALUE_NOT_ALLOWED`).
 *   - Withdrawals are blocked outright (`WITHDRAWAL_BLOCKED`).
 *   - Drawdown breach checks fail the call (`ACCOUNT_BREACHED`).
 *
 * Denials surface as `PaxeerWalletError` with stable `code` set to one of
 * the values in `FundedDenyCode`. The PWA renders a friendly deny modal
 * keyed off that code.
 *
 * Lets the existing swap SDK work unchanged for funded users on
 * whitelisted contracts: every `new ethers.Contract(addr, abi, signer)`
 * write call ends up calling `signer.sendTransaction(tx)`, and we route
 * through the funded endpoint.
 */

import { ethers } from 'ethers';
import type { PaxeerWallet } from './sdk';
import type { TxRequest } from './sdk/types';

const WAIT_FOR_RECEIPT_MS = 2_500;

export interface FundedSignerOptions {
    /** REST client wired to `connect.paxportwallet.com`. */
    client: PaxeerWallet;
    /** Funded EOA address. From `/v1/funded/me`. */
    address: string;
    /** RPC URL — same one self-custody / embedded use. */
    rpcUrl: string;
    /** Chain ID. Defaults to Paxeer Mainnet (125). */
    chainId?: number;
}

export class FundedSigner extends ethers.AbstractSigner<ethers.JsonRpcProvider> {
    private readonly client: PaxeerWallet;
    private readonly address: string;
    private readonly chainId?: number;

    constructor(options: FundedSignerOptions, provider?: ethers.JsonRpcProvider) {
        super(provider ?? new ethers.JsonRpcProvider(options.rpcUrl));
        this.client = options.client;
        this.address = options.address;
        this.chainId = options.chainId;
    }

    // ── Identity ────────────────────────────────────────────────────────
    async getAddress(): Promise<string> {
        return this.address;
    }

    connect(provider: null | ethers.Provider): ethers.AbstractSigner {
        if (provider === null) {
            return new FundedSigner(
                {
                    client: this.client,
                    address: this.address,
                    rpcUrl: '',
                    chainId: this.chainId,
                },
                this.provider!,
            );
        }
        if (provider instanceof ethers.JsonRpcProvider) {
            return new FundedSigner(
                {
                    client: this.client,
                    address: this.address,
                    rpcUrl: '',
                    chainId: this.chainId,
                },
                provider,
            );
        }
        throw new Error('FundedSigner.connect() only supports JsonRpcProvider');
    }

    // ── Signing surface ────────────────────────────────────────────────

    async signTransaction(_tx: ethers.TransactionRequest): Promise<string> {
        throw new Error(
            'FundedSigner.signTransaction is not supported — funded keys are server-side. ' +
            'Use sendTransaction() to sign and broadcast in one call.',
        );
    }

    async signMessage(message: string | Uint8Array): Promise<string> {
        const payload =
            typeof message === 'string' ? message : ethers.hexlify(message);
        const r = await this.client.signFundedMessage(payload);
        return r.signature;
    }

    async signTypedData(
        _domain: ethers.TypedDataDomain,
        _types: Record<string, ethers.TypedDataField[]>,
        _value: Record<string, unknown>,
    ): Promise<string> {
        throw new Error(
            'FundedSigner.signTypedData is not yet supported by the funded API.',
        );
    }

    // ── Transaction sending ─────────────────────────────────────────────

    async sendTransaction(
        tx: ethers.TransactionRequest,
    ): Promise<ethers.TransactionResponse> {
        const populated = await this.populateTransaction(tx);

        const toStr = (v: unknown): string | undefined =>
            v === undefined || v === null ? undefined : String(v);

        const sdkReq: TxRequest = {
            to: (populated.to as `0x${string}` | undefined) ?? undefined,
            value: toStr(populated.value),
            data: (populated.data as `0x${string}` | undefined) ?? undefined,
            gas: toStr(populated.gasLimit),
            maxFeePerGas: toStr(populated.maxFeePerGas),
            maxPriorityFeePerGas: toStr(populated.maxPriorityFeePerGas),
            nonce:
                typeof populated.nonce === 'number' ? populated.nonce : undefined,
            chainId:
                typeof populated.chainId === 'bigint'
                    ? Number(populated.chainId)
                    : (populated.chainId as number | undefined) ?? this.chainId,
        };

        // Funded endpoint — policy-gated server-side. Denials throw
        // `PaxeerWalletError` with a `FundedDenyCode` in `error.code`.
        const r = await this.client.sendFundedTransaction(sdkReq);

        return this.makeTxResponse(r.tx_hash, populated, r.chain_id);
    }

    // ── Internal: synthetic TransactionResponse ─────────────────────────
    private makeTxResponse(
        hash: string,
        tx: ethers.TransactionRequest,
        chainId: number,
    ): ethers.TransactionResponse {
        const provider = this.provider!;
        const toBigInt = (v: unknown): bigint | null => {
            if (v === undefined || v === null) return null;
            try {
                return BigInt(String(v));
            } catch {
                return null;
            }
        };
        const response: Partial<ethers.TransactionResponse> & {
            hash: string;
            wait: ethers.TransactionResponse['wait'];
        } = {
            hash,
            to: (tx.to as string | null) ?? null,
            from: this.address,
            nonce: typeof tx.nonce === 'number' ? tx.nonce : 0,
            gasLimit: toBigInt(tx.gasLimit) ?? 0n,
            gasPrice: 0n,
            maxFeePerGas: toBigInt(tx.maxFeePerGas),
            maxPriorityFeePerGas: toBigInt(tx.maxPriorityFeePerGas),
            value: toBigInt(tx.value) ?? 0n,
            chainId: BigInt(chainId),
            data: (tx.data as string | undefined) ?? '0x',
            // Paxeer Network doesn't return receipts for successful txs as a
            // matter of policy — see EmbeddedSigner for the rationale.
            wait: async (
                _confirms?: number,
                _timeout?: number,
            ): Promise<ethers.TransactionReceipt | null> => {
                try {
                    return await Promise.race([
                        provider.waitForTransaction(hash, 1, WAIT_FOR_RECEIPT_MS),
                        new Promise<null>((resolve) =>
                            setTimeout(() => resolve(null), WAIT_FOR_RECEIPT_MS),
                        ),
                    ]);
                } catch {
                    return null;
                }
            },
        };
        return response as ethers.TransactionResponse;
    }
}
