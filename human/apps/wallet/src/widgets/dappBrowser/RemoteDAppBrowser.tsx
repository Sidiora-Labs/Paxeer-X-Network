'use client';

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
} from 'react';
import {
  ArrowLeft,
  ArrowRight,
  Globe,
  LayoutGrid,
  Loader2,
  Lock,
  Plus,
  RotateCw,
  X,
} from 'lucide-react';
import { DAppConfirmationDialog } from '@/components/DAppConfirmationDialog';
import {
  confirmConnectRequest,
  confirmDAppRequest,
  toSignableBytes,
  type DAppConfirmationRequest,
} from '@/lib/dappConfirm';
import { PAXEER_CONFIG } from '@/lib/constants';
import {
  executeDappRpc,
  type BridgeWallet,
  type DappRpcTransport,
} from '@/lib/eip1193-bridge';
import {
  parseRemoteBrowserEvents,
  parseRemoteBrowserSession,
  parseRemoteBrowserState,
  remoteBrowserRequest,
  type RemoteBrowserSession,
  type RemoteBrowserState,
  type RemoteRpcRequest,
} from '@/lib/remote-browser';
import { useWalletActions, useWalletState } from '@/providers/WalletProvider';

interface RemoteDAppBrowserProps {
  url: string;
  title: string;
  onBack: () => void;
  hideHeader?: boolean;
}

interface ConfirmationState {
  request: DAppConfirmationRequest;
  resolve: (approved: boolean) => void;
}

function hostname(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

function rpcError(caught: unknown): { code: number; message: string; data?: unknown } {
  const source =
    caught && typeof caught === 'object'
      ? (caught as { code?: unknown; message?: unknown; data?: unknown })
      : {};
  return {
    code: typeof source.code === 'number' ? source.code : -32603,
    message:
      typeof source.message === 'string' ? source.message : 'The wallet request failed.',
    ...(source.data !== undefined ? { data: source.data } : {}),
  };
}

export function RemoteDAppBrowser({
  url,
  title,
  onBack,
  hideHeader,
}: RemoteDAppBrowserProps) {
  const { activeAccount, isLocked } = useWalletState();
  const { getSigner } = useWalletActions();
  const surfaceRef = useRef<HTMLDivElement>(null);
  const sessionRef = useRef<RemoteBrowserSession | null>(null);
  const stateRef = useRef<RemoteBrowserState | null>(null);
  const confirmationRef = useRef<ConfirmationState | null>(null);
  const [session, setSession] = useState<RemoteBrowserSession | null>(null);
  const [browserState, setBrowserState] = useState<RemoteBrowserState | null>(null);
  const [address, setAddress] = useState(url);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showTabs, setShowTabs] = useState(false);
  const [confirmation, setConfirmation] = useState<ConfirmationState | null>(null);
  const [retryGeneration, setRetryGeneration] = useState(0);
  const chainHex = useMemo(
    () => `0x${PAXEER_CONFIG.chainId.toString(16)}`,
    [],
  );

  stateRef.current = browserState;
  sessionRef.current = session;
  confirmationRef.current = confirmation;

  const requestConfirmation = useCallback(
    (request: DAppConfirmationRequest) =>
      new Promise<boolean>(resolve => setConfirmation({ request, resolve })),
    [],
  );

  const settleConfirmation = useCallback((approved: boolean) => {
    confirmationRef.current?.resolve(approved);
    confirmationRef.current = null;
    setConfirmation(null);
  }, []);

  const sessionRequest = useCallback(
    async (
      suffix: string,
      options: {
        method?: 'GET' | 'POST' | 'DELETE';
        body?: unknown;
        signal?: AbortSignal;
      } = {},
    ) => {
      const activeSession = sessionRef.current;
      if (!activeSession) throw new Error('The secure browser session is unavailable.');
      return remoteBrowserRequest(
        `sessions/${activeSession.sessionId}${suffix ? `/${suffix}` : ''}`,
        { ...options, token: activeSession.token },
      );
    },
    [],
  );

  const pushProviderEvent = useCallback(
    async (event: string, payload?: unknown) => {
      await sessionRequest('provider-event', {
        method: 'POST',
        body: { event, payload },
      });
    },
    [sessionRequest],
  );

  const bridgeWallet = useMemo<BridgeWallet>(
    () => ({
      getAccounts: async () =>
        !isLocked && activeAccount?.address ? [activeAccount.address] : [],
      getChainIdHex: () => chainHex,
      getSigner,
      personalSign: async message => {
        const signer = await getSigner();
        return signer.signMessage(toSignableBytes(message));
      },
      signTypedDataV4: async (_address, typedDataJson) => {
        const signer = await getSigner();
        const typedData = JSON.parse(typedDataJson) as {
          domain: Record<string, unknown>;
          types: Record<string, Array<{ name: string; type: string }>>;
          message: Record<string, unknown>;
        };
        const { EIP712Domain: _domain, ...types } = typedData.types;
        return signer.signTypedData(typedData.domain, types, typedData.message);
      },
      sendTransaction: async transaction => {
        const signer = await getSigner();
        return (await signer.sendTransaction(transaction)).hash;
      },
      confirmRequest: (method, params, origin) =>
        confirmDAppRequest(method, params, {
          origin,
          activeAddress: activeAccount?.address,
          confirm: requestConfirmation,
        }),
    }),
    [
      activeAccount?.address,
      chainHex,
      getSigner,
      isLocked,
      requestConfirmation,
    ],
  );

  const transport = useMemo<DappRpcTransport>(
    () => ({
      requestConnection: origin =>
        confirmConnectRequest(origin, {
          activeAddress: activeAccount?.address,
          confirm: requestConfirmation,
        }),
      pushEvent: pushProviderEvent,
    }),
    [activeAccount?.address, pushProviderEvent, requestConfirmation],
  );

  const handleRpc = useCallback(
    async (request: RemoteRpcRequest) => {
      const activeSession = sessionRef.current;
      const activeState = stateRef.current;
      if (!activeSession || !activeState) return;
      const contextMatches =
        request.tabId === activeState.activeTabId &&
        request.navigationGeneration === activeState.navigationGeneration &&
        (() => {
          try {
            return new URL(activeState.url).origin === request.origin;
          } catch {
            return false;
          }
        })();

      let payload: { result?: unknown; error?: ReturnType<typeof rpcError> };
      if (!contextMatches) {
        payload = {
          error: {
            code: 4100,
            message: 'The browser context changed before the request was reviewed.',
          },
        };
      } else {
        try {
          payload = {
            result: await executeDappRpc(
              request.method,
              request.params as Parameters<typeof executeDappRpc>[1],
              bridgeWallet,
              transport,
              request.origin,
            ),
          };
        } catch (caught) {
          payload = { error: rpcError(caught) };
        }
      }

      try {
        await remoteBrowserRequest(
          `sessions/${activeSession.sessionId}/rpc/${request.id}`,
          {
            method: 'POST',
            token: activeSession.token,
            body: payload,
          },
        );
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : 'Wallet response failed.');
      }
    },
    [bridgeWallet, transport],
  );

  useEffect(() => {
    const controller = new AbortController();
    let activeSession: RemoteBrowserSession | null = null;

    const start = async () => {
      setLoading(true);
      setError(null);
      const rect = surfaceRef.current?.getBoundingClientRect();
      try {
        const response = await remoteBrowserRequest('sessions', {
          method: 'POST',
          body: {
            url,
            width: Math.round(rect?.width || window.innerWidth),
            height: Math.round(rect?.height || Math.max(540, window.innerHeight - 64)),
            deviceScaleFactor: 2,
          },
          signal: controller.signal,
        });
        activeSession = parseRemoteBrowserSession(await response.json());
        sessionRef.current = activeSession;
        stateRef.current = activeSession;
        setSession(activeSession);
        setBrowserState(activeSession);
        setAddress(activeSession.url || url);

        void (async () => {
          let cursor = 0;
          while (!controller.signal.aborted && sessionRef.current === activeSession) {
            try {
              const response = await remoteBrowserRequest(
                `sessions/${activeSession.sessionId}/events?cursor=${cursor}`,
                { token: activeSession.token, signal: controller.signal },
              );
              const batch = parseRemoteBrowserEvents(await response.json());
              cursor = batch.cursor;
              for (const event of batch.events) {
                if (event.type === 'state') {
                  stateRef.current = event.state;
                  setBrowserState(event.state);
                  setAddress(event.state.url);
                  setLoading(false);
                } else if (event.type === 'rpc') {
                  void handleRpc(event.request);
                } else if (event.type === 'notice') {
                  setError(event.message);
                }
              }
            } catch (caught) {
              if (!controller.signal.aborted) {
                setError(
                  caught instanceof Error
                    ? caught.message
                    : 'The browser event channel disconnected.',
                );
              }
              break;
            }
          }
        })();
      } catch (caught) {
        if (!controller.signal.aborted) {
          setError(
            caught instanceof Error ? caught.message : 'The secure browser could not start.',
          );
          setLoading(false);
        }
      }
    };

    void start();
    return () => {
      controller.abort();
      settleConfirmation(false);
      const closing = activeSession ?? sessionRef.current;
      if (closing) {
        void remoteBrowserRequest(`sessions/${closing.sessionId}`, {
          method: 'DELETE',
          token: closing.token,
        }).catch(() => undefined);
      }
      sessionRef.current = null;
      stateRef.current = null;
    };
  }, [handleRpc, retryGeneration, settleConfirmation, url]);

  useEffect(() => {
    if (!session || !activeAccount?.address || isLocked) return;
    void pushProviderEvent('accountsChanged', [activeAccount.address]).catch(() => undefined);
  }, [activeAccount?.address, isLocked, pushProviderEvent, session]);

  useEffect(() => {
    if (!session || !isLocked) return;
    void pushProviderEvent('accountsChanged', [])
      .catch(() => undefined)
      .finally(onBack);
  }, [isLocked, onBack, pushProviderEvent, session]);

  const navigate = useCallback(
    async (action: 'goto' | 'back' | 'forward' | 'reload', target?: string) => {
      setLoading(true);
      setError(null);
      try {
        const response = await sessionRequest('navigation', {
          method: 'POST',
          body: { action, ...(target ? { url: target } : {}) },
        });
        const next = parseRemoteBrowserState(await response.json());
        stateRef.current = next;
        setBrowserState(next);
        setAddress(next.url);
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : 'Navigation failed.');
        setLoading(false);
      }
    },
    [sessionRequest],
  );

  const submitAddress = useCallback(
    (event: FormEvent) => {
      event.preventDefault();
      const value = address.trim();
      if (!value) return;
      void navigate('goto', value.startsWith('http') ? value : `https://${value}`);
    },
    [address, navigate],
  );

  const switchTab = useCallback(
    async (tabId: string) => {
      try {
        const response = await sessionRequest(`tabs/${tabId}/activate`, {
          method: 'POST',
          body: {},
        });
        const next = parseRemoteBrowserState(await response.json());
        setBrowserState(next);
        stateRef.current = next;
        setShowTabs(false);
        setLoading(true);
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : 'Could not switch tabs.');
      }
    },
    [sessionRequest],
  );

  const closeTab = useCallback(
    async (tabId: string) => {
      try {
        const response = await sessionRequest(`tabs/${tabId}`, { method: 'DELETE' });
        const next = parseRemoteBrowserState(await response.json());
        setBrowserState(next);
        stateRef.current = next;
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : 'Could not close the tab.');
      }
    },
    [sessionRequest],
  );

  const createTab = useCallback(async () => {
    try {
      const response = await sessionRequest('tabs', {
        method: 'POST',
        body: { url },
      });
      const body = (await response.json()) as { state?: unknown };
      const next = parseRemoteBrowserState(body.state);
      setBrowserState(next);
      stateRef.current = next;
      setShowTabs(false);
      setLoading(true);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not create a tab.');
    }
  }, [sessionRequest, url]);

  const visibleUrl = browserState?.url || address || url;
  const streamUrl = session
    ? `${session.streamPath}#token=${encodeURIComponent(session.token)}`
    : null;

  return (
    <div className="flex h-[100dvh] flex-col bg-pax-bg">
      {hideHeader ? (
        <div className="absolute left-3 top-3 z-30 safe-area-pt">
          <button
            onClick={onBack}
            className="rounded-full bg-black/60 p-2 backdrop-blur-sm press-scale"
            aria-label="Close browser"
          >
            <ArrowLeft className="h-4 w-4 text-white" />
          </button>
        </div>
      ) : (
        <header className="z-20 shrink-0 bg-pax-bg/95 backdrop-blur-xl safe-area-pt">
          <div className="flex h-12 items-center gap-0.5 px-1.5">
            <button
              onClick={() => void navigate('back')}
              disabled={!session}
              className="shrink-0 p-2 press-scale disabled:opacity-25"
              aria-label="Back"
            >
              <ArrowLeft className="h-4 w-4 text-white/70" />
            </button>
            <button
              onClick={() => void navigate('forward')}
              disabled={!session}
              className="shrink-0 p-2 press-scale disabled:opacity-25"
              aria-label="Forward"
            >
              <ArrowRight className="h-4 w-4 text-white/70" />
            </button>
            <button
              onClick={() => void navigate('reload')}
              disabled={!session}
              className="shrink-0 p-2 press-scale disabled:opacity-25"
              aria-label="Reload"
            >
              <RotateCw
                className={`h-3.5 w-3.5 text-white/45 ${loading ? 'animate-spin' : ''}`}
              />
            </button>
            <form
              onSubmit={submitAddress}
              className="mx-1 flex min-w-0 flex-1 items-center gap-1.5 rounded-xl bg-white/[0.07] px-3 py-1.5"
            >
              <Lock className="h-2.5 w-2.5 shrink-0 text-pax-success/70" />
              <input
                value={address}
                onChange={event => setAddress(event.target.value)}
                onFocus={event => event.currentTarget.select()}
                aria-label="Browser address"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                className="min-w-0 flex-1 bg-transparent font-mono text-[11px] tracking-tight text-white/65 outline-none"
              />
            </form>
            <button
              onClick={() => setShowTabs(true)}
              disabled={!session}
              className="relative shrink-0 p-2 press-scale disabled:opacity-25"
              aria-label="Browser tabs"
            >
              <LayoutGrid className="h-4 w-4 text-white/45" />
              {(browserState?.tabs.length ?? 0) > 1 && (
                <span className="absolute right-0 top-0 flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-pax-accent px-0.5 text-[9px] font-bold leading-none text-black">
                  {browserState?.tabs.length}
                </span>
              )}
            </button>
            <button
              onClick={onBack}
              className="shrink-0 p-2 press-scale"
              aria-label="Close browser"
            >
              <X className="h-4 w-4 text-white/45" />
            </button>
          </div>
        </header>
      )}

      <div ref={surfaceRef} className="relative min-h-0 flex-1 overflow-hidden bg-black">
        {streamUrl && (
          <iframe
            key={session?.sessionId}
            src={streamUrl}
            title={`Remote browser showing ${hostname(visibleUrl)}`}
            className="h-full w-full bg-black"
            sandbox="allow-forms allow-pointer-lock allow-scripts"
            referrerPolicy="no-referrer"
            allow="fullscreen 'none'; microphone 'none'; camera 'none'; clipboard-read 'none'; clipboard-write 'none'"
            onLoad={() => setLoading(false)}
          />
        )}

        {loading && (
          <div className="absolute inset-0 flex items-center justify-center bg-pax-bg">
            <div className="flex flex-col items-center gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-pax-accent" />
              <p className="text-xs text-pax-muted">
                Opening secure browser for {hostname(visibleUrl)}…
              </p>
            </div>
          </div>
        )}

        {error && (
          <div className="absolute inset-x-3 bottom-3 z-20 rounded-2xl bg-[var(--color-surface-raised)] p-4 shadow-xl">
            <p className="text-sm font-semibold">Browser interrupted</p>
            <p className="mt-1 text-xs leading-relaxed text-pax-muted">{error}</p>
            <div className="mt-3 flex gap-2">
              <button
                onClick={() => setRetryGeneration(value => value + 1)}
                className="flex-1 rounded-xl bg-pax-accent px-3 py-2 text-xs font-semibold text-black press-scale"
              >
                Retry
              </button>
              <button
                onClick={() => setError(null)}
                className="rounded-xl bg-white/[0.06] px-3 py-2 text-xs font-medium press-scale"
              >
                Dismiss
              </button>
            </div>
          </div>
        )}
      </div>

      {showTabs && (
        <div
          className="fixed inset-0 z-50 flex flex-col justify-end bg-black/70 backdrop-blur-sm"
          onClick={() => setShowTabs(false)}
        >
          <div
            className="flex max-h-[72vh] flex-col rounded-t-3xl bg-[var(--color-surface-raised)] p-5 pb-8 safe-area-pb"
            onClick={event => event.stopPropagation()}
          >
            <div className="mb-4 flex shrink-0 items-center justify-between">
              <span className="text-sm font-semibold">
                {browserState?.tabs.length ?? 0}{' '}
                {(browserState?.tabs.length ?? 0) === 1 ? 'Tab' : 'Tabs'}
              </span>
              <div className="flex gap-1">
                <button
                  onClick={() => void createTab()}
                  className="rounded-full bg-white/[0.06] p-1.5 press-scale"
                  aria-label="New browser tab"
                >
                  <Plus className="h-3.5 w-3.5 text-white/60" />
                </button>
                <button
                  onClick={() => setShowTabs(false)}
                  className="rounded-full bg-white/[0.06] p-1.5 press-scale"
                  aria-label="Close tab switcher"
                >
                  <X className="h-3.5 w-3.5 text-white/50" />
                </button>
              </div>
            </div>
            <div className="min-h-0 flex-1 space-y-2 overflow-y-auto">
              {browserState?.tabs.map(tab => (
                <div
                  key={tab.id}
                  className={`flex items-center rounded-2xl ${
                    tab.active ? 'bg-pax-accent/10' : 'bg-white/[0.04]'
                  }`}
                >
                  <button
                    onClick={() => void switchTab(tab.id)}
                    className="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 text-left press-scale"
                  >
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-white/[0.05]">
                      <Globe className="h-3.5 w-3.5 text-white/30" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-xs font-medium text-white/90">
                        {tab.title || title}
                      </p>
                      <p className="truncate font-mono text-[10px] text-white/35">
                        {hostname(tab.url)}
                      </p>
                    </div>
                  </button>
                  {tab.active ? (
                    <span className="shrink-0 pr-3 text-[9px] font-semibold uppercase tracking-wide text-pax-accent">
                      Active
                    </span>
                  ) : (
                    <button
                      onClick={() => void closeTab(tab.id)}
                      className="mr-3 rounded-full p-1 press-scale hover:bg-white/10"
                      aria-label={`Close ${tab.title || hostname(tab.url)}`}
                    >
                      <X className="h-3 w-3 text-white/35" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      <DAppConfirmationDialog
        request={confirmation?.request ?? null}
        onApprove={() => settleConfirmation(true)}
        onReject={() => settleConfirmation(false)}
      />
    </div>
  );
}
