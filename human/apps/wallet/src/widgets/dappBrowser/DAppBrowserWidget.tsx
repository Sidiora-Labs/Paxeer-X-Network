'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { useWalletState, useWalletActions } from '@/providers/WalletProvider';
import { Eip1193Bridge, type BridgeWallet } from '@/lib/eip1193-bridge';
import {
    confirmDAppRequest,
    confirmConnectRequest,
    parseSignRequest,
    toSignableBytes,
    type DAppConfirmationRequest,
} from '@/lib/dappConfirm';
import { PAXEER_CONFIG, getActiveRpcUrl } from '@/lib/constants';
import { ethers } from 'ethers';
import { ArrowLeft, Globe, LayoutGrid, Loader2, Lock, RotateCw, X } from 'lucide-react';
import { DAppConfirmationDialog } from '@/components/DAppConfirmationDialog';
import type { AppRoute } from '@/widgets/shell/useAppRoute';
import {
    isNativeDAppBrowserAvailable,
    openNativeDAppBrowser,
    sendNativeRpcResponse,
    pushNativeEvent,
    onNativeRpcRequest,
    onNativeBrowserClosed,
    type NativeRpcRequest,
} from '@/lib/native-dapp-browser';
import {
    createScramjetBrowser,
    type ScramjetBrowserSession,
} from '@/lib/scramjet-browser';
import {
    dappTabsRepository,
    type DappTabRecord,
} from '@/platform/storage/repositories';
import {
    grantDappPermission,
    hasDappPermission,
    recordDappMethod,
} from '@/lib/dapp-permissions';

const MAX_TABS = 8;

type BrowserTab = DappTabRecord;

function loadTabs(): BrowserTab[] {
    return dappTabsRepository.read();
}

function saveTabs(tabs: BrowserTab[]) {
    dappTabsRepository.write(tabs);
}

function upsertTab(tabs: BrowserTab[], url: string, title: string): BrowserTab[] {
    const existing = tabs.find(t => t.url === url);
    const updated: BrowserTab = { id: existing?.id ?? Math.random().toString(36).slice(2), url, title, lastVisited: Date.now() };
    return [updated, ...tabs.filter(t => t.url !== url)].slice(0, MAX_TABS);
}

function safeHostname(url: string): string {
    try { return new URL(url).hostname; } catch { return url; }
}

function usesDirectIframe(url: string): boolean {
    try {
        const hostname = new URL(url).hostname.toLowerCase();
        return hostname === 'kindlelaunch.com' || hostname === 'www.kindlelaunch.com';
    } catch {
        return false;
    }
}

function tryReadIframeUrl(iframe: HTMLIFrameElement | null): string | null {
    try { return iframe?.contentWindow?.location?.href ?? null; } catch { return null; }
}

interface DAppBrowserWidgetProps {
    url: string;
    title: string;
    onBack: () => void;
    onNavigate?: (route: AppRoute) => void;
    hideHeader?: boolean;
}

function NativeDAppBrowser({ url, title, onBack }: DAppBrowserWidgetProps) {
    const { activeAccount, isLocked } = useWalletState();
    const { getSigner } = useWalletActions();
    const chainHex = '0x' + PAXEER_CONFIG.chainId.toString(16);
    const launchedRef = useRef(false);
    const onBackRef = useRef(onBack);
    onBackRef.current = onBack;
    const expectedOrigin = new URL(url).origin;
    const [confirmation, setConfirmation] = useState<{
        request: DAppConfirmationRequest;
        resolve: (approved: boolean) => void;
    } | null>(null);

    const requestConfirmation = useCallback((request: DAppConfirmationRequest) => (
        new Promise<boolean>((resolve) => setConfirmation({ request, resolve }))
    ), []);

    const approveConfirmation = useCallback(() => {
        confirmation?.resolve(true);
        setConfirmation(null);
    }, [confirmation]);

    const rejectConfirmation = useCallback(() => {
        confirmation?.resolve(false);
        setConfirmation(null);
    }, [confirmation]);

    const requestNativeConfirmation = useCallback(async (
        method: string,
        params: unknown[],
    ) => {
        await confirmDAppRequest(method, params, {
            origin: expectedOrigin,
            activeAddress: activeAccount?.address,
            confirm: requestConfirmation,
        });
    }, [expectedOrigin, activeAccount?.address, requestConfirmation]);

    const handleRpc = useCallback(async (
        method: string,
        params: unknown[],
        origin: string,
    ): Promise<unknown> => {
        if (origin !== expectedOrigin) {
            throw { code: 4100, message: 'Native dApp origin does not match the open page.' };
        }
        const account = isLocked ? undefined : activeAccount?.address;
        switch (method) {
            case 'eth_requestAccounts': {
                if (!account) return [];
                if (!hasDappPermission(origin, account, PAXEER_CONFIG.chainId)) {
                    const approved = await confirmConnectRequest(origin, {
                        activeAddress: account,
                        confirm: requestConfirmation,
                    });
                    if (!approved) throw { code: 4001, message: 'User rejected the request.' };
                    grantDappPermission(origin, account, PAXEER_CONFIG.chainId);
                }
                recordDappMethod(origin, method);
                return [account];
            }
            case 'eth_accounts':
                return account && hasDappPermission(origin, account, PAXEER_CONFIG.chainId)
                    ? [account]
                    : [];
            case 'eth_chainId': return chainHex;
            case 'net_version': return String(PAXEER_CONFIG.chainId);
            case 'wallet_switchEthereumChain': {
                const requested = (params as any)?.[0]?.chainId;
                if (requested && requested !== chainHex) throw { code: 4902, message: 'Chain not supported' };
                return null;
            }
            case 'wallet_addEthereumChain':
                throw { code: 4200, message: 'Adding chains is not supported.' };
            case 'personal_sign':
            case 'eth_sign': {
                if (!account || !hasDappPermission(origin, account, PAXEER_CONFIG.chainId)) {
                    throw { code: 4100, message: 'Connect this account first.' };
                }
                await requestNativeConfirmation(method, params);
                const { message } = parseSignRequest(method, params);
                const signer = await getSigner();
                const result = await signer.signMessage(toSignableBytes(message));
                recordDappMethod(origin, method);
                return result;
            }
            case 'eth_signTypedData':
            case 'eth_signTypedData_v3':
            case 'eth_signTypedData_v4': {
                if (!account || !hasDappPermission(origin, account, PAXEER_CONFIG.chainId)) {
                    throw { code: 4100, message: 'Connect this account first.' };
                }
                await requestNativeConfirmation(method, params);
                const { message: typedDataJson } = parseSignRequest(method, params);
                const signer = await getSigner();
                const td = JSON.parse(typedDataJson);
                const { EIP712Domain: _d, ...types } = td.types;
                const result = await signer.signTypedData(td.domain, types, td.message);
                recordDappMethod(origin, method);
                return result;
            }
            case 'eth_sendTransaction': {
                if (!account || !hasDappPermission(origin, account, PAXEER_CONFIG.chainId)) {
                    throw { code: 4100, message: 'Connect this account first.' };
                }
                const [txReq] = params as ethers.TransactionRequest[];
                await requestNativeConfirmation(method, params);
                const signer = await getSigner();
                const result = (await signer.sendTransaction(txReq)).hash;
                recordDappMethod(origin, method);
                return result;
            }
            default: {
                const READ_ONLY_METHODS = new Set([
                    'eth_blockNumber', 'eth_getBalance', 'eth_getCode', 'eth_getTransactionCount',
                    'eth_getBlockByNumber', 'eth_getBlockByHash', 'eth_getTransactionByHash',
                    'eth_getTransactionReceipt', 'eth_call', 'eth_estimateGas', 'eth_gasPrice',
                    'eth_maxPriorityFeePerGas', 'eth_feeHistory', 'eth_getLogs',
                    'eth_getBlockReceipts', 'net_listening', 'net_peerCount',
                    'web3_clientVersion', 'web3_sha3',
                ]);
                if (!READ_ONLY_METHODS.has(method)) {
                    throw { code: 4200, message: `Method not supported: ${method}` };
                }
                const provider = new ethers.JsonRpcProvider(getActiveRpcUrl());
                return provider.send(method, params as any[]);
            }
        }
    }, [activeAccount, isLocked, getSigner, chainHex, expectedOrigin, requestConfirmation, requestNativeConfirmation]);

    const handleRpcRef = useRef(handleRpc);
    handleRpcRef.current = handleRpc;

    useEffect(() => {
        if (launchedRef.current) return;
        launchedRef.current = true;
        openNativeDAppBrowser({
            url,
            title,
            address: !isLocked ? activeAccount?.address || '' : '',
            chainId: chainHex,
            rpcUrl: getActiveRpcUrl(),
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useEffect(() => {
        const rpcCleanup = onNativeRpcRequest(async (data: NativeRpcRequest) => {
            const { id, method, params: paramsJson, origin } = data;
            if (
                typeof id !== 'string' ||
                id.length < 1 ||
                id.length > 128 ||
                typeof method !== 'string' ||
                !/^[a-z][a-zA-Z0-9_]{1,63}$/.test(method) ||
                typeof paramsJson !== 'string' ||
                paramsJson.length > 64 * 1_024
            ) {
                await sendNativeRpcResponse(id || 'invalid', undefined, {
                    code: -32600,
                    message: 'Invalid native wallet request.',
                });
                return;
            }
            let parsedParams: unknown[];
            try {
                const parsed = JSON.parse(paramsJson) as unknown;
                if (!Array.isArray(parsed)) throw new Error('Params must be an array');
                parsedParams = parsed;
            } catch {
                await sendNativeRpcResponse(id, undefined, {
                    code: -32602,
                    message: 'Invalid native request params.',
                });
                return;
            }
            try {
                const result = await handleRpcRef.current(method, parsedParams, origin);
                await sendNativeRpcResponse(id, result);
            } catch (err: any) {
                await sendNativeRpcResponse(id, undefined, { code: err?.code ?? -32603, message: err?.message ?? 'Internal error' });
            }
        });
        const closeCleanup = onNativeBrowserClosed(() => onBackRef.current());
        return () => { rpcCleanup?.then(h => h.remove()); closeCleanup?.then(h => h.remove()); };
    }, []);

    useEffect(() => {
        void pushNativeEvent(
            'accountsChanged',
            !isLocked && activeAccount?.address ? [activeAccount.address] : [],
        );
    }, [activeAccount?.address, isLocked]);

    return (
        <div className="flex flex-col h-screen bg-pax-bg">
            <div className="flex-1 flex items-center justify-center">
                <div className="flex flex-col items-center gap-3">
                    <Loader2 className="w-8 h-8 animate-spin text-pax-accent" />
                    <p className="text-xs text-pax-muted">DApp browser open</p>
                </div>
            </div>
            <DAppConfirmationDialog
                request={confirmation?.request ?? null}
                onApprove={approveConfirmation}
                onReject={rejectConfirmation}
            />
        </div>
    );
}

function IframeDAppBrowser({ url, title, onBack, hideHeader }: DAppBrowserWidgetProps) {
    const { activeAccount, isLocked } = useWalletState();
    const { getSigner } = useWalletActions();
    const directIframe = usesDirectIframe(url);
    const iframeRef = useRef<HTMLIFrameElement>(null);
    const bridgeRef = useRef<Eip1193Bridge | null>(null);
    const proxySessionRef = useRef<ScramjetBrowserSession | null>(null);
    const chainHex = '0x' + PAXEER_CONFIG.chainId.toString(16);

    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [retryToken, setRetryToken] = useState(0);
    const historyDepthRef = useRef(0);
    const loadCountRef = useRef(0);
    const [canGoBack, setCanGoBack] = useState(false);
    const [displayUrl, setDisplayUrl] = useState(url);
    const [showTabs, setShowTabs] = useState(false);
    const [tabs, setTabs] = useState<BrowserTab[]>(() => { const stored = loadTabs(); return upsertTab(stored, url, title); });
    const entryUrlRef = useRef(url);
    const [confirmation, setConfirmation] = useState<{
        request: DAppConfirmationRequest;
        resolve: (approved: boolean) => void;
    } | null>(null);

    const requestConfirmation = useCallback((request: DAppConfirmationRequest) => (
        new Promise<boolean>((resolve) => {
            setConfirmation({ request, resolve });
        })
    ), []);

    const approveConfirmation = useCallback(() => {
        confirmation?.resolve(true);
        setConfirmation(null);
    }, [confirmation]);

    const rejectConfirmation = useCallback(() => {
        confirmation?.resolve(false);
        setConfirmation(null);
    }, [confirmation]);

    useEffect(() => {
        entryUrlRef.current = url;
        setTabs(prev => { const next = upsertTab(prev, url, title); saveTabs(next); return next; });
        setDisplayUrl(url); historyDepthRef.current = 0; loadCountRef.current = 0; setCanGoBack(false);
    }, [url, title]);

    const makeBridgeWallet = useCallback((): BridgeWallet => ({
        getAccounts: async () => !isLocked && activeAccount?.address ? [activeAccount.address] : [],
        getChainIdHex: () => chainHex,
        getSigner: () => getSigner(),
        confirmRequest: (method, params, origin) => confirmDAppRequest(method, params, {
            origin,
            activeAddress: activeAccount?.address,
            confirm: requestConfirmation,
        }),
        personalSign: async (message: string) => { const signer = await getSigner(); return signer.signMessage(toSignableBytes(message)); },
        signTypedDataV4: async (_addr: string, typedDataJson: string) => {
            const signer = await getSigner();
            const td = JSON.parse(typedDataJson);
            const { EIP712Domain: _d, ...types } = td.types;
            return signer.signTypedData(td.domain, types, td.message);
        },
        sendTransaction: async (txReq: ethers.TransactionRequest) => { const signer = await getSigner(); return (await signer.sendTransaction(txReq)).hash; },
    }), [activeAccount, isLocked, getSigner, chainHex, requestConfirmation]);

    useEffect(() => {
        const bridge = new Eip1193Bridge(makeBridgeWallet(), [
            window.location.origin,
            'https://app.hyperpax.xyz', 'https://app.hyperpaxeer.com',
            'https://paxscan.io', 'https://www.kindlelaunch.com', 'https://kindlelaunch.com',
        ]);
        bridgeRef.current = bridge;
        if (directIframe) {
            bridge.registerIframe(iframeRef.current);
            bridge.start();
        }
        bridge.setProviderEventHandler((event, payload) => {
            proxySessionRef.current?.emitProviderEvent(event, payload);
        });
        bridge.onConnectRequest = async (info) => {
            const approved = await confirmConnectRequest(info.origin, {
                activeAddress: activeAccount?.address,
                confirm: requestConfirmation,
            });
            if (approved && activeAccount?.address) {
                bridge.accountsChanged([activeAccount.address]);
            }
            return approved;
        };
        return () => {
            bridge.stop();
        };
    }, [makeBridgeWallet, activeAccount?.address, requestConfirmation, directIframe]);

    useEffect(() => {
        if (directIframe) {
            setLoading(true);
            setLoadError(null);
            return;
        }
        const iframe = iframeRef.current;
        if (!iframe) return;
        let disposed = false;

        setLoading(true);
        setLoadError(null);
        void createScramjetBrowser({
            iframe,
            onUrlChange: (nextUrl) => {
                if (disposed) return;
                setDisplayUrl(nextUrl);
                setLoading(false);
                loadCountRef.current += 1;
                if (loadCountRef.current > 1) {
                    historyDepthRef.current += 1;
                    setCanGoBack(true);
                }
                setTabs(prev => {
                    const next = upsertTab(prev, nextUrl, safeHostname(nextUrl));
                    saveTabs(next);
                    return next;
                });
            },
            onProviderRequest: async ({ method, params, origin }) => {
                const bridge = bridgeRef.current;
                if (!bridge) {
                    throw new Error('The wallet provider is not ready.');
                }
                return bridge.requestFromProvider(method, params, origin);
            },
        }).then((session) => {
            if (disposed) {
                session.destroy();
                return;
            }
            proxySessionRef.current = session;
            session.go(entryUrlRef.current);
        }).catch((caught) => {
            if (disposed) return;
            setLoading(false);
            setLoadError(
                caught instanceof Error
                    ? caught.message
                    : 'The in-app browser could not start.',
            );
        });

        return () => {
            disposed = true;
            proxySessionRef.current?.destroy();
            proxySessionRef.current = null;
        };
    }, [retryToken, directIframe]);

    useEffect(() => {
        bridgeRef.current?.accountsChanged(
            !isLocked && activeAccount?.address ? [activeAccount.address] : [],
        );
    }, [activeAccount?.address, isLocked]);

    const handleLoad = () => {
        if (directIframe) {
            bridgeRef.current?.updateIframeOrigin(iframeRef.current);
            setLoading(false);
            return;
        }
        if (proxySessionRef.current) setLoading(false);
    };

    const handleBack = () => {
        if (directIframe) {
            onBack();
            return;
        }
        if (!canGoBack) return;
        setLoading(true);
        historyDepthRef.current = Math.max(0, historyDepthRef.current - 1);
        if (historyDepthRef.current === 0) setCanGoBack(false);
        proxySessionRef.current?.back();
    };

    const handleReload = () => {
        setLoading(true);
        if (directIframe) {
            iframeRef.current?.setAttribute('src', url);
            return;
        }
        proxySessionRef.current?.reload();
    };

    const handleSwitchTab = (tab: BrowserTab) => {
        if (tab.url === displayUrl) { setShowTabs(false); return; }
        historyDepthRef.current = 0; loadCountRef.current = 0; setCanGoBack(false); setDisplayUrl(tab.url); setLoading(true);
        proxySessionRef.current?.go(tab.url);
        setTabs(prev => { const next = upsertTab(prev, tab.url, tab.title); saveTabs(next); return next; });
        setShowTabs(false);
    };

    const handleCloseTab = (tabId: string, e: React.MouseEvent) => {
        e.stopPropagation();
        setTabs(prev => { const next = prev.filter(t => t.id !== tabId); saveTabs(next); return next; });
    };

    const hostname = safeHostname(displayUrl);

    return (
        <div className="flex flex-col h-screen bg-pax-bg">
            {hideHeader ? (
                <div className="absolute top-3 left-3 z-20 safe-area-pt">
                    <button onClick={onBack} className="p-2 rounded-full bg-black/50 backdrop-blur-sm press-scale"><ArrowLeft className="w-4 h-4 text-white" /></button>
                </div>
            ) : (
                <header className="bg-pax-bg/95 backdrop-blur-xl shrink-0 safe-area-pt  ">
                    <div className="flex items-center gap-1 px-1.5 h-12">
                        <button onClick={handleBack} disabled={!directIframe && !canGoBack} className="p-2 press-scale disabled:opacity-25 shrink-0" aria-label="Back"><ArrowLeft className="w-4 h-4 text-white/70" /></button>
                        <button onClick={handleReload} className="p-2 press-scale shrink-0" aria-label="Reload"><RotateCw className={`w-3.5 h-3.5 text-white/45 ${loading ? 'animate-spin' : ''}`} /></button>
                        <div className="flex-1 flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-white/[0.07]   min-w-0 mx-1">
                            <Lock className="w-2.5 h-2.5 text-white/25 shrink-0" />
                            <span className="text-[11px] text-white/55 truncate font-mono tracking-tight">{hostname}</span>
                        </div>
                        {!directIframe && (
                            <button onClick={() => setShowTabs(true)} className="relative p-2 press-scale shrink-0" aria-label="Tabs">
                                <LayoutGrid className="w-4 h-4 text-white/45" />
                                {tabs.length > 1 && <span className="absolute -top-0.5 -right-0.5 min-w-[14px] h-[14px] rounded-full bg-pax-accent text-black text-[9px] font-bold flex items-center justify-center px-0.5 leading-none">{tabs.length}</span>}
                            </button>
                        )}
                        <button onClick={onBack} className="p-2 press-scale shrink-0" aria-label="Close"><X className="w-4 h-4 text-white/45" /></button>
                    </div>
                </header>
            )}

            <div className="flex-1 relative overflow-hidden">
                {loading && (
                    <div className="absolute inset-0 flex items-center justify-center bg-pax-bg z-10">
                        <div className="flex flex-col items-center gap-3">
                            <Loader2 className="w-8 h-8 animate-spin text-pax-accent" />
                            <p className="text-xs text-pax-muted">Loading {hostname}…</p>
                        </div>
                    </div>
                )}
                {loadError && (
                    <div className="absolute inset-0 z-20 flex items-center justify-center bg-pax-bg px-8">
                        <div className="flex max-w-xs flex-col items-center gap-4 text-center">
                            <Globe className="h-8 w-8 text-white/30" />
                            <div>
                                <p className="text-sm font-semibold text-white">Browser unavailable</p>
                                <p className="mt-1 text-xs text-pax-muted">{loadError}</p>
                            </div>
                            <button
                                type="button"
                                onClick={() => setRetryToken(value => value + 1)}
                                className="rounded-xl bg-pax-accent px-4 py-2 text-xs font-semibold text-black press-scale"
                            >
                                Try again
                            </button>
                        </div>
                    </div>
                )}
                <iframe
                    ref={iframeRef}
                    src={directIframe ? url : 'about:blank'}
                    title={title}
                    className="h-full w-full"
                    onLoad={handleLoad}
                    allow="autoplay; clipboard-read; clipboard-write; fullscreen; payment"
                    sandbox="allow-downloads allow-forms allow-modals allow-orientation-lock allow-pointer-lock allow-popups allow-popups-to-escape-sandbox allow-presentation allow-same-origin allow-scripts"
                />
            </div>

            {showTabs && (
                <div className="fixed inset-0 z-50 flex flex-col justify-end bg-black/70 backdrop-blur-sm" onClick={() => setShowTabs(false)}>
                    <div className="bg-[var(--color-surface-raised)] rounded-t-3xl p-5 pb-8 max-h-[72vh] flex flex-col safe-area-pb" onClick={e => e.stopPropagation()}>
                        <div className="flex items-center justify-between mb-4 shrink-0">
                            <span className="text-sm font-semibold">{tabs.length} {tabs.length === 1 ? 'Tab' : 'Tabs'}</span>
                            <button onClick={() => setShowTabs(false)} className="p-1.5 rounded-full bg-white/5 press-scale"><X className="w-3.5 h-3.5 text-white/50" /></button>
                        </div>
                        <div className="overflow-y-auto flex-1 space-y-2 pr-0.5">
                            {tabs.map(tab => {
                                const isActive = tab.url === displayUrl;
                                return (
                                    <div key={tab.id} className={`w-full flex items-center rounded-2xl transition-colors ${isActive ? 'bg-pax-accent/10' : 'bg-white/[0.04]'}`}>
                                        <button onClick={() => handleSwitchTab(tab)} className="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 text-left press-scale">
                                            <div className="w-8 h-8 rounded-xl bg-white/5 flex items-center justify-center shrink-0"><Globe className="w-3.5 h-3.5 text-white/25" /></div>
                                            <div className="flex-1 min-w-0">
                                                <p className="text-xs font-medium text-white/90 truncate">{tab.title}</p>
                                                <p className="text-[10px] text-white/30 font-mono truncate">{safeHostname(tab.url)}</p>
                                            </div>
                                        </button>
                                        {isActive ? (
                                            <span className="pr-3 text-[9px] font-semibold text-pax-accent uppercase tracking-wide shrink-0">Active</span>
                                        ) : (
                                            <button onClick={e => handleCloseTab(tab.id, e)} className="mr-3 p-1 rounded-full hover:bg-white/10 press-scale shrink-0" aria-label="Close tab"><X className="w-3 h-3 text-white/30" /></button>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                </div>
            )}
            <DAppConfirmationDialog
                request={confirmation?.request ?? null}
                onApprove={approveConfirmation}
                onReject={rejectConfirmation}
            />
        </div>
    );
}

export function DAppBrowserWidget(props: DAppBrowserWidgetProps) {
    if (isNativeDAppBrowserAvailable()) return <NativeDAppBrowser {...props} />;
    return <IframeDAppBrowser {...props} />;
}
