/**
 * `EmbeddedSigner` — an `ethers.Signer` that delegates transaction signing
 * **and** broadcast to the Paxeer Embedded Wallet API
 * (`POST /v1/wallet/send` on `connect.paxportwallet.com`).
 *
 * Lets the existing swap SDK in `@/lib/swap/` work unchanged for embedded
 * users: every `new ethers.Contract(addr, abi, signer)` write call ends up
 * calling `signer.sendTransaction(tx)`, which we override below to route
 * through the embedded API instead of producing a raw signed tx locally.
 *
 * Behaviour notes that matter for the rest of the codebase:
 *
 *   - **Read calls** go through a normal `JsonRpcProvider` against the
 *     same Paxeer RPC the self-custody path uses, so balances, allowances,
 *     and quotes resolve identically across custody models.
 *
 *   - **`signTransaction` / `signMessage` / `signTypedData`** — the
 *     embedded API only exposes `signMessage` over EIP-191. Methods that
 *     would expose private-key material (`signTransaction`) throw on
 *     purpose; the swap SDK never calls them.
 *
 *   - **`tx.wait()` is best-effort.** Paxeer Network nodes are designed
 *     not to return receipts for successful transactions — the convention
 *     is "if no revert within ~2 s, treat as success". Our `wait()`
 *     mirrors that: it polls `getTransaction` briefly, then resolves
 *     `null` so callers don't block forever. The swap executor already
 *     handles `wait()` returning null / throwing by ignoring it and
 *     returning the tx hash immediately.
 */

import { ethers } from 'ethers';
import type { PaxeerWallet } from './sdk';
import type { TxRequest } from './sdk/types';

/**
 * How long we'll poll for a receipt before giving up and resolving null.
 * Two seconds matches the Paxeer Network "if it didn't revert in ~2s,
 * treat it as confirmed" convention. Any longer and we just hold the UI
 * hostage on a wallet that already succeeded.
 */
const WAIT_FOR_RECEIPT_MS = 2_500;

export interface EmbeddedSignerOptions {
    /** REST client wired to `connect.paxportwallet.com`. */
    client: PaxeerWallet;
    /** EOA address the embedded service signs for, fetched from `/v1/wallet/me`. */
    address: string;
    /** RPC URL — same one self-custody uses, for read calls and tx polling. */
    rpcUrl: string;
    /** Chain ID. Defaults to Paxeer Mainnet (125). */
    chainId?: number;
}

export class EmbeddedSigner extends ethers.AbstractSigner<ethers.JsonRpcProvider> {
    private readonly client: PaxeerWallet;
    private readonly address: string;
    private readonly chainId?: number;

    constructor(options: EmbeddedSignerOptions, provider?: ethers.JsonRpcProvider) {
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
        // ethers calls connect() to clone us with a new provider when wiring
        // up `Contract.connect(provider)`. Always return a JsonRpcProvider
        // instance so our cast above stays valid.
        if (provider === null) {
            return new EmbeddedSigner(
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
            return new EmbeddedSigner(
                {
                    client: this.client,
                    address: this.address,
                    rpcUrl: '',
                    chainId: this.chainId,
                },
                provider,
            );
        }
        // Other provider types are an unsupported path — embedded callers
        // always run against a JsonRpcProvider in practice.
        throw new Error('EmbeddedSigner.connect() only supports JsonRpcProvider');
    }

    // ── Signing surface ────────────────────────────────────────────────

    /**
     * The embedded API holds the private key server-side and never returns
     * it. `signTransaction` would imply local signing of a raw tx for
     * later broadcast — we cannot do that. Callers should use
     * `sendTransaction()` (which signs + broadcasts in one round-trip) or
     * `signMessage()` (which calls the dedicated EIP-191 endpoint).
     */
    async signTransaction(_tx: ethers.TransactionRequest): Promise<string> {
        throw new Error(
            'EmbeddedSigner.signTransaction is not supported — embedded keys are server-side. ' +
            'Use sendTransaction() to sign and broadcast in one call.',
        );
    }

    async signMessage(message: string | Uint8Array): Promise<string> {
        // Match `Wallet.signMessage` behavior: hex-encode bytes so the API
        // server can hash them with the correct EIP-191 prefix.
        const payload =
            typeof message === 'string' ? message : ethers.hexlify(message);
        const r = await this.client.signMessage(payload);
        return r.signature;
    }

    async signTypedData(
        _domain: ethers.TypedDataDomain,
        _types: Record<string, ethers.TypedDataField[]>,
        _value: Record<string, unknown>,
    ): Promise<string> {
        // Not used by the swap SDK. When the embedded service exposes a
        // typed-data signing endpoint we'll hook it up here.
        throw new Error(
            'EmbeddedSigner.signTypedData is not yet supported by the embedded API.',
        );
    }

    // ── Transaction sending ─────────────────────────────────────────────

    async sendTransaction(
        tx: ethers.TransactionRequest,
    ): Promise<ethers.TransactionResponse> {
        const populated = await this.populateTransaction(tx);

        // Translate ethers TransactionRequest → SDK TxRequest. The SDK
        // accepts decimal strings for value/gas (or bigints) and ignores
        // fields it doesn't recognise. Ethers v6 uses `null` (not
        // `undefined`) to signal "no value", so we coerce both shapes.
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

        const r = await this.client.sendTransaction(sdkReq);

        // Build a minimal TransactionResponse so `await tx.wait()` on the
        // caller side doesn't blow up. Ethers requires an associated
        // provider — we pass our read provider which can poll the chain if
        // ever the receipt does come back.
        return this.makeTxResponse(r.tx_hash, populated, r.chain_id);
    }

    // ── Internal: synthetic TransactionResponse ─────────────────────────
    private makeTxResponse(
        hash: string,
        tx: ethers.TransactionRequest,
        chainId: number,
    ): ethers.TransactionResponse {
        const provider = this.provider!;
        // We hand-roll a TransactionResponse-like object. ethers' real
        // TransactionResponse class has a long list of fields; we fill in
        // the ones the swap SDK actually reads (`hash`, `wait`) plus a few
        // that downstream callers commonly inspect.
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
            // Tx metadata — best-effort. Most consumers never read these.
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
            // PaxeerNetwork doesn't return receipts for successful txs as a
            // matter of policy. Best-effort poll, then resolve `null` so
            // callers fall through to "treat as success".
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
