import { ethers } from 'ethers';
import { PAXEER_CONFIG, getActiveRpcUrl } from '@/lib/constants';
import { parseSignRequest } from '@/lib/dappConfirm';
import {
    grantDappPermission,
    hasDappPermission,
    permissionForOrigin,
    recordDappMethod,
    revokeDappPermission,
} from '@/lib/dapp-permissions';

export type RpcParams = unknown[] | Record<string, unknown> | undefined;

export interface BridgeWallet {
    getAccounts: () => Promise<string[]>;
    getChainIdHex: () => string;
    getSigner: () => Promise<ethers.Signer>;
    personalSign: (message: string, address: string) => Promise<string>;
    signTypedDataV4: (address: string, typedData: string) => Promise<string>;
    sendTransaction: (tx: ethers.TransactionRequest) => Promise<string>;
    confirmRequest?: (
        method: string,
        params: RpcParams,
        origin: string,
    ) => Promise<void>;
}

export interface DappRpcTransport {
    requestConnection: (origin: string) => Promise<boolean>;
    pushEvent: (event: string, payload?: unknown) => void | Promise<void>;
}

type IframeRef = HTMLIFrameElement | null;
type RequestId = string | number;

interface ParsedRequest {
    id: RequestId;
    method: string;
    params: RpcParams;
}

const REQUEST_TIMEOUT_MS = 60_000;
const REPLAY_WINDOW_MS = 5 * 60_000;
const MAX_REQUEST_BYTES = 64 * 1_024;
const METHOD = /^[a-z][a-zA-Z0-9_]{1,63}$/;

const READ_ONLY_METHODS = new Set([
    'eth_blockNumber',
    'eth_getBalance',
    'eth_getCode',
    'eth_getTransactionCount',
    'eth_getBlockByNumber',
    'eth_getBlockByHash',
    'eth_getTransactionByHash',
    'eth_getTransactionReceipt',
    'eth_call',
    'eth_estimateGas',
    'eth_gasPrice',
    'eth_maxPriorityFeePerGas',
    'eth_feeHistory',
    'eth_getLogs',
    'eth_getBlockReceipts',
    'net_listening',
    'net_peerCount',
    'web3_clientVersion',
    'web3_sha3',
]);

function hexChainId(): string {
    return `0x${PAXEER_CONFIG.chainId.toString(16)}`;
}

function rpcError(code: number, message: string): { code: number; message: string } {
    return { code, message };
}

function parseRequest(
    id: unknown,
    method: unknown,
    params: unknown,
): ParsedRequest {
    if (
        (typeof id !== 'string' && typeof id !== 'number') ||
        (typeof id === 'string' && (id.length < 1 || id.length > 128)) ||
        (typeof id === 'number' && !Number.isSafeInteger(id))
    ) {
        throw rpcError(-32600, 'Invalid request id.');
    }
    if (typeof method !== 'string' || !METHOD.test(method)) {
        throw rpcError(-32600, 'Invalid request method.');
    }
    if (
        params !== undefined &&
        !Array.isArray(params) &&
        (typeof params !== 'object' || params === null)
    ) {
        throw rpcError(-32602, 'Invalid request params.');
    }
    let encoded: string;
    try {
        encoded = JSON.stringify({ id, method, params });
    } catch {
        throw rpcError(-32602, 'Request params are not serializable.');
    }
    if (new TextEncoder().encode(encoded).byteLength > MAX_REQUEST_BYTES) {
        throw rpcError(-32602, 'Request exceeds the size limit.');
    }
    return { id, method, params: params as RpcParams };
}

async function withTimeout<T>(operation: Promise<T>): Promise<T> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timeout = new Promise<never>((_, reject) => {
        timer = setTimeout(
            () => reject(rpcError(-32000, 'Wallet request timed out.')),
            REQUEST_TIMEOUT_MS,
        );
    });
    try {
        return await Promise.race([operation, timeout]);
    } finally {
        if (timer) clearTimeout(timer);
    }
}

async function activeAuthorizedAccount(
    wallet: BridgeWallet,
    origin: string,
): Promise<string> {
    const account = (await wallet.getAccounts())[0];
    if (
        !account ||
        !hasDappPermission(origin, account, PAXEER_CONFIG.chainId)
    ) {
        throw rpcError(4100, 'Unauthorized: connect this account first.');
    }
    return account;
}

export async function executeDappRpc(
    method: string,
    params: RpcParams,
    wallet: BridgeWallet,
    transport: DappRpcTransport,
    origin: string,
): Promise<unknown> {
    switch (method) {
        case 'eth_requestAccounts': {
            const accounts = await wallet.getAccounts();
            const account = accounts[0];
            if (!account) throw rpcError(4100, 'No active wallet account.');
            if (!hasDappPermission(origin, account, PAXEER_CONFIG.chainId)) {
                const approved = await transport.requestConnection(origin);
                if (!approved) throw rpcError(4001, 'User rejected the request.');
                grantDappPermission(origin, account, PAXEER_CONFIG.chainId);
            }
            recordDappMethod(origin, method);
            await transport.pushEvent('connect', { chainId: hexChainId() });
            await transport.pushEvent('accountsChanged', [account]);
            return [account];
        }
        case 'eth_accounts': {
            const accounts = await wallet.getAccounts();
            const account = accounts[0];
            return account &&
                hasDappPermission(origin, account, PAXEER_CONFIG.chainId)
                ? [account]
                : [];
        }
        case 'eth_chainId':
            return wallet.getChainIdHex();
        case 'net_version':
            return String(PAXEER_CONFIG.chainId);
        case 'wallet_switchEthereumChain': {
            const requested = Array.isArray(params)
                ? (params[0] as { chainId?: unknown } | undefined)?.chainId
                : undefined;
            if (requested !== undefined && requested !== hexChainId()) {
                throw rpcError(4902, 'Chain not supported.');
            }
            return null;
        }
        case 'wallet_addEthereumChain':
            throw rpcError(4200, 'Adding chains is not supported.');
        case 'personal_sign':
        case 'eth_sign': {
            const active = await activeAuthorizedAccount(wallet, origin);
            const parsed = parseSignRequest(method, params);
            if (parsed.address && parsed.address.toLowerCase() !== active.toLowerCase()) {
                throw rpcError(4100, 'The requested signing account is not connected.');
            }
            if (wallet.confirmRequest) {
                await wallet.confirmRequest(method, params, origin);
            }
            const signature = await wallet.personalSign(parsed.message, active);
            recordDappMethod(origin, method);
            return signature;
        }
        case 'eth_signTypedData':
        case 'eth_signTypedData_v3':
        case 'eth_signTypedData_v4': {
            const active = await activeAuthorizedAccount(wallet, origin);
            const parsed = parseSignRequest(method, params);
            if (parsed.address && parsed.address.toLowerCase() !== active.toLowerCase()) {
                throw rpcError(4100, 'The requested signing account is not connected.');
            }
            if (wallet.confirmRequest) {
                await wallet.confirmRequest(method, params, origin);
            }
            const signature = await wallet.signTypedDataV4(active, parsed.message);
            recordDappMethod(origin, method);
            return signature;
        }
        case 'eth_sendTransaction': {
            const active = await activeAuthorizedAccount(wallet, origin);
            if (!Array.isArray(params) || params.length !== 1) {
                throw rpcError(-32602, 'eth_sendTransaction requires one transaction.');
            }
            const transaction = params[0] as ethers.TransactionRequest;
            if (
                transaction.from &&
                String(transaction.from).toLowerCase() !== active.toLowerCase()
            ) {
                throw rpcError(4100, 'The transaction account is not connected.');
            }
            if (wallet.confirmRequest) {
                await wallet.confirmRequest(method, params, origin);
            }
            const hash = await wallet.sendTransaction({
                ...transaction,
                from: active,
                chainId: PAXEER_CONFIG.chainId,
            });
            recordDappMethod(origin, method);
            return hash;
        }
        default: {
            if (!READ_ONLY_METHODS.has(method)) {
                throw rpcError(4200, `Method not supported: ${method}`);
            }
            const provider = new ethers.JsonRpcProvider(getActiveRpcUrl());
            return provider.send(method, Array.isArray(params) ? params : []);
        }
    }
}

export type ConnectRequestInfo = { origin: string };

export class Eip1193Bridge {
    private readonly wallet: BridgeWallet;
    private readonly allowedOrigins: ReadonlySet<string>;
    private listener: ((event: MessageEvent) => void) | null = null;
    private readonly iframes = new Set<IframeRef>();
    private readonly iframeOrigins = new Map<IframeRef, string>();
    private readonly seenRequests = new Map<string, number>();
    private providerEventHandler:
        | ((event: string, payload?: unknown) => void)
        | null = null;
    onConnectRequestHandler:
        | ((info: ConnectRequestInfo) => Promise<boolean>)
        | null = null;

    constructor(wallet: BridgeWallet, allowedOrigins: string[] = []) {
        this.wallet = wallet;
        this.allowedOrigins = new Set(allowedOrigins);
    }

    set onConnectRequest(
        handler: ((info: ConnectRequestInfo) => Promise<boolean>) | null,
    ) {
        this.onConnectRequestHandler = handler;
    }

    async requestConnection(origin: string): Promise<boolean> {
        if (!this.onConnectRequestHandler) {
            throw rpcError(4100, 'Unauthorized: connection approval is unavailable.');
        }
        return this.onConnectRequestHandler({ origin });
    }

    async requestFromProvider(
        method: string,
        params: RpcParams,
        origin: string,
    ): Promise<unknown> {
        const parsed = parseRequest(
            `provider-${crypto.randomUUID()}`,
            method,
            params,
        );
        return withTimeout(
            executeDappRpc(
                parsed.method,
                parsed.params,
                this.wallet,
                this,
                origin,
            ),
        );
    }

    setProviderEventHandler(
        handler: ((event: string, payload?: unknown) => void) | null,
    ): void {
        this.providerEventHandler = handler;
    }

    registerIframe(iframe: IframeRef): void {
        if (!iframe) return;
        this.iframes.add(iframe);
        this.updateIframeOrigin(iframe);
    }

    updateIframeOrigin(iframe: IframeRef): void {
        if (!iframe) return;
        const src = iframe.getAttribute('src');
        try {
            const origin = src
                ? new URL(src, window.location.href).origin
                : window.location.origin;
            if (origin === 'null') {
                this.iframeOrigins.delete(iframe);
                return;
            }
            this.iframeOrigins.set(iframe, origin);
        } catch {
            this.iframeOrigins.delete(iframe);
        }
    }

    unregisterIframe(iframe: IframeRef): void {
        this.iframes.delete(iframe);
        this.iframeOrigins.delete(iframe);
    }

    private registeredIframeFor(event: MessageEvent): HTMLIFrameElement | null {
        for (const iframe of this.iframes) {
            if (
                iframe &&
                iframe.contentWindow === event.source &&
                this.iframeOrigins.get(iframe) === event.origin
            ) {
                return iframe;
            }
        }
        return null;
    }

    private rememberRequest(origin: string, id: RequestId): void {
        const now = Date.now();
        for (const [key, timestamp] of this.seenRequests) {
            if (now - timestamp > REPLAY_WINDOW_MS) this.seenRequests.delete(key);
        }
        const key = `${origin}:${String(id)}`;
        if (this.seenRequests.has(key)) {
            throw rpcError(-32600, 'Duplicate or replayed request id.');
        }
        this.seenRequests.set(key, now);
    }

    start(): void {
        if (this.listener) return;
        this.listener = (event: MessageEvent) => {
            void this.handleMessage(event);
        };
        window.addEventListener('message', this.listener);
    }

    private async handleMessage(event: MessageEvent): Promise<void> {
        if (
            (this.allowedOrigins.size > 0 && !this.allowedOrigins.has(event.origin)) ||
            !this.registeredIframeFor(event)
        ) {
            return;
        }
        const data = event.data as Record<string, unknown> | null;
        if (!data || typeof data !== 'object') return;
        if (data.target === 'sidiora:eip1193' && data.type === 'ready') {
            this.pushEventDex('chainChanged', hexChainId());
            return;
        }

        const dex = data.target === 'sidiora:eip1193' && data.type === 'request';
        const native = data.type === 'SIDIORA_EIP1193_REQUEST';
        if (!dex && !native) return;
        const requestRecord =
            dex && data.request && typeof data.request === 'object'
                ? data.request as Record<string, unknown>
                : data;
        let parsed: ParsedRequest | null = null;
        try {
            parsed = parseRequest(data.id, requestRecord.method, requestRecord.params);
            this.rememberRequest(event.origin, parsed.id);
            const result = await withTimeout(
                executeDappRpc(
                    parsed.method,
                    parsed.params,
                    this.wallet,
                    this,
                    event.origin,
                ),
            );
            this.respond(event, dex, parsed.id, result);
        } catch (caught) {
            const error = caught as { code?: unknown; message?: unknown; data?: unknown };
            this.respondError(
                event,
                dex,
                parsed?.id ?? (
                    typeof data.id === 'string' || typeof data.id === 'number'
                        ? data.id
                        : 'invalid'
                ),
                {
                    code: typeof error.code === 'number' ? error.code : -32603,
                    message:
                        typeof error.message === 'string'
                            ? error.message
                            : 'Internal wallet error.',
                    ...(error.data !== undefined ? { data: error.data } : {}),
                },
            );
        }
    }

    private respond(
        event: MessageEvent,
        dex: boolean,
        id: RequestId,
        result: unknown,
    ): void {
        (event.source as WindowProxy | null)?.postMessage(
            dex
                ? { target: 'sidiora:eip1193', type: 'response', id, result }
                : { type: 'SIDIORA_EIP1193_RESPONSE', id, result },
            { targetOrigin: event.origin },
        );
    }

    private respondError(
        event: MessageEvent,
        dex: boolean,
        id: RequestId,
        error: { code: number; message: string; data?: unknown },
    ): void {
        (event.source as WindowProxy | null)?.postMessage(
            dex
                ? { target: 'sidiora:eip1193', type: 'response', id, error }
                : { type: 'SIDIORA_EIP1193_RESPONSE', id, error },
            { targetOrigin: event.origin },
        );
    }

    stop(): void {
        if (this.listener) {
            window.removeEventListener('message', this.listener);
        }
        this.listener = null;
        this.seenRequests.clear();
        this.providerEventHandler = null;
    }

    pushEventDex(event: string, params?: unknown): void {
        for (const iframe of this.iframes) {
            const targetOrigin = this.iframeOrigins.get(iframe);
            if (!targetOrigin) continue;
            iframe?.contentWindow?.postMessage(
                { target: 'sidiora:eip1193', type: 'event', event, params },
                targetOrigin,
            );
        }
    }

    pushEventFutures(event: string, payload?: unknown): void {
        for (const iframe of this.iframes) {
            const targetOrigin = this.iframeOrigins.get(iframe);
            if (!targetOrigin) continue;
            iframe?.contentWindow?.postMessage(
                { type: 'SIDIORA_EIP1193_EVENT', event, payload },
                targetOrigin,
            );
        }
    }

    pushEvent(event: string, payload?: unknown): void {
        this.pushEventDex(event, payload);
        this.pushEventFutures(event, payload);
        this.providerEventHandler?.(event, payload);
    }

    accountsChanged(accounts: string[]): void {
        this.pushEvent('accountsChanged', accounts);
    }

    chainChanged(chainId: string): void {
        this.pushEvent('chainChanged', chainId);
    }

    isOriginConnected(origin: string): boolean {
        return permissionForOrigin(origin) !== null;
    }

    revokeOrigin(origin: string): void {
        revokeDappPermission(origin);
        this.pushEvent('accountsChanged', []);
        this.pushEvent('disconnect', { code: 4900, message: 'Permission revoked.' });
    }
}
