'use client';

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { getAddress } from 'viem';
import { useAccount, useConnect, useDisconnect, useSwitchChain, type Connector } from 'wagmi';
import { Button, NoticeBar } from '@/ui/primitives';
import { ModalFrame } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import { useAuthModal } from './auth-modal-store';
import { useWalletRuntime } from '@/core/wallet-sdk/runtime';
import { WalletLogin, type LoginConsent } from '@/core/wallet-sdk/auth';
import { connectorProvider } from '@/core/wallet-sdk/client';
import { BROWSER_WALLET_NAME, detectInjectedWallet, type InjectedPresence } from '@/core/wallet-sdk/injected-detection';
import { measureWalletOperation } from '@/core/wallet-sdk/measurement';
import { accountEpoch, clearAccountSession, installAccountSession, loginChallenge, loginProof, subscribeAccount } from '@/widgets/profile/account-transport';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { walletConnectConfigured } from '@/core/wagmi-config';
import SignInWalletRow, { MoreWalletsRow, WalletMark } from './sign-in-wallet-row';
import { orderWalletChoices, readRecentConnectorId, rememberConnector } from './wallet-choices';
import EmbeddedSignIn from './embedded-sign-in';
import { PAXEER_CONNECTOR_ID } from '@/core/wallet-sdk/embedded_sdk/embedded';
import { embeddedWalletConfigured } from '@/core/wagmi-config';
import { createSingleMount } from './single-mount';

const LOGIN_STATEMENT = 'Sign in to Sidiora.';
export const NO_BROWSER_WALLET_NOTE = 'No browser wallet found. Install one or continue with email';
export const NO_BROWSER_WALLET_NOTE_WALLET_ONLY = 'No browser wallet found. Install one to sign in';

/** The no-wallet note only points at the embedded wallet when this deployment offers it. */
export function noBrowserWalletNote(embeddedOffered: boolean): string {
  return embeddedOffered ? NO_BROWSER_WALLET_NOTE : NO_BROWSER_WALLET_NOTE_WALLET_ONLY;
}
export const CONNECTION_CANCELLED = 'Connection cancelled';
export const CONNECTION_FAILED = "Couldn't connect. Check your wallet and try again";
const WALLET_NOT_READY = 'Your wallet is still loading. Try again in a moment';
const PRIMARY_ROWS = 3;
const useSignInOwner = createSingleMount();

type Step = 'connect' | 'network' | 'sign';

/**
 * Sign-in dialog driven by the global auth-modal store. Flow: pick a wallet
 * (wagmi), or sign in with email / a social account through the Paxeer
 * Embedded Wallet when the deployment configures it -> switch to Paxeer when
 * needed -> review the EIP-4361 message -> sign -> session installed via
 * account-transport. The embedded connector never appears as a wallet row;
 * it is connected automatically once its Supabase session exists.
 */
export default function SignInModal() {
  const owner = useSignInOwner();
  return owner ? <SignInDialog /> : null;
}

function SignInDialog() {
  const { open, reason, close } = useAuthModal();
  const { connectors, connectAsync, status: connectStatus } = useConnect();
  const { address, isConnected, chainId, connector: activeConnector, status: accountStatus } = useAccount();
  const { disconnectAsync } = useDisconnect();
  const { switchChainAsync } = useSwitchChain();
  const runtime = useWalletRuntime();
  const { session } = useAccountSession();
  const moreId = useId();

  const [recentId, setRecentId] = useState<string | null>(null);
  // Injected-provider probe results by connector uid. Empty on the server and on
  // the first client render; filled by an effect after mount, so the markup never
  // depends on `window` during render.
  const [injectedPresence, setInjectedPresence] = useState<Record<string, InjectedPresence>>({});
  // Read after mount (the flag is browser-only), so the first render never depends on it.
  const [embeddedOffered, setEmbeddedOffered] = useState(false);
  const [showMore, setShowMore] = useState(false);
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [review, setReview] = useState<LoginConsent | null>(null);
  const consentResolver = useRef<((value: boolean) => void) | null>(null);
  const operation = useRef(false);
  const epoch = useRef(0);

  useEffect(() => {
    epoch.current++;
    consentResolver.current?.(false); consentResolver.current = null; setReview(null);
    if (!open) return;
    setError(null); setShowMore(false);
    setRecentId(readRecentConnectorId());
    setEmbeddedOffered(embeddedWalletConfigured);
  }, [open]);

  const walletConnectors = useMemo(() => connectors.filter(connector => connector.id !== PAXEER_CONNECTOR_ID), [connectors]);

  useEffect(() => {
    if (!open) return;
    let live = true;
    for (const connector of walletConnectors) {
      if (connector.type !== 'injected') continue;
      void detectInjectedWallet(connector).then(presence => {
        if (!live) return;
        setInjectedPresence(current => {
          const known = current[connector.uid];
          return known && known.present === presence.present && known.name === presence.name ? current : { ...current, [connector.uid]: presence };
        });
      });
    }
    return () => { live = false; };
  }, [open, walletConnectors]);

  useEffect(() => { if (open && session) close(); }, [open, session, close]);
  useEffect(() => {
    const unsubscribe = runtime.sessions.subscribe(() => { consentResolver.current?.(false); consentResolver.current = null; setReview(null); });
    return unsubscribe;
  }, [runtime.sessions]);

  const injectedConnectors = useMemo(() => walletConnectors.filter(connector => connector.type === 'injected'), [walletConnectors]);
  const genericDetected = injectedConnectors.some(connector => connector.id === 'injected' && injectedPresence[connector.uid]?.present === true);
  // Every injected connector was probed and none has a provider: no browser wallet.
  const noBrowserWallet = injectedConnectors.length > 0 && injectedConnectors.every(connector => injectedPresence[connector.uid]?.present === false);
  const offered = useMemo(() => walletConnectors.filter(connector => connector.type !== 'injected' || injectedPresence[connector.uid]?.present !== false), [walletConnectors, injectedPresence]);
  const choices = useMemo(() => orderWalletChoices(offered, recentId, genericDetected), [offered, recentId, genericDetected]);
  const displayName = (connector: Connector) => connector.type === 'injected'
    ? injectedPresence[connector.uid]?.name ?? (connector.id === 'injected' ? BROWSER_WALLET_NAME : connector.name)
    : connector.name;
  // An injected row is selectable only once its provider has been seen.
  const probing = (connector: Connector) => connector.type === 'injected' && injectedPresence[connector.uid]?.present !== true;
  const primary = choices.slice(0, PRIMARY_ROWS);
  const extra = choices.slice(PRIMARY_ROWS);
  const busy = pending !== null || connectStatus === 'pending' || accountStatus === 'connecting' || accountStatus === 'reconnecting';
  const step: Step = !isConnected || !address ? 'connect' : chainId !== 125 ? 'network' : 'sign';

  const guardedClose = useCallback(() => { if (!operation.current) close(); }, [close]);

  const connectWallet = async (connector: Connector) => {
    if (operation.current || busy || probing(connector)) return;
    operation.current = true;
    const version = epoch.current;
    setPending(connector.uid); setError(null);
    try {
      await connectorProvider(connector);
      const result = await measureWalletOperation('connector.connect', `connector:${connector.id}`, () => connectAsync({ connector }));
      if (version !== epoch.current) return;
      rememberConnector(connector.id); setRecentId(connector.id);
      setError(result.chainId === 125 ? null : 'Your wallet is on another network. Switch to Paxeer to continue');
    } catch (failure: unknown) {
      if (version !== epoch.current) return;
      runtime.clearSigningAccess();
      const notFound = failure instanceof Error && (failure.message === 'ProviderNotFound' || failure.name === 'ProviderNotFoundError');
      const code = typeof failure === 'object' && failure !== null && 'code' in failure ? (failure as { code?: unknown }).code : undefined;
      const rejected = code === 4001 || (failure instanceof Error && (failure.name === 'UserRejectedRequestError' || /rejected|denied|cancel/i.test(failure.message)));
      setError(notFound ? `${displayName(connector)} isn't installed in this browser`
        : rejected ? CONNECTION_CANCELLED
        : CONNECTION_FAILED);
    } finally { operation.current = false; setPending(null); }
  };

  const switchNetwork = async () => {
    if (operation.current || busy) return;
    operation.current = true;
    const version = epoch.current;
    setPending('network'); setError(null);
    try { await switchChainAsync({ chainId: 125 }); }
    catch { if (version === epoch.current) setError("Couldn't switch network. Pick Paxeer in your wallet and try again"); }
    finally { operation.current = false; setPending(null); }
  };

  const changeWallet = async () => {
    if (operation.current) return;
    operation.current = true;
    setPending('disconnect'); setError(null);
    try { runtime.clearSigningAccess(); clearAccountSession(); await disconnectAsync(); }
    catch { setError("Couldn't disconnect. Disconnect from your wallet app and try again"); }
    finally { operation.current = false; setPending(null); }
  };

  const signIn = async () => {
    const signer = runtime.wallet;
    if (operation.current || !address || chainId !== 125) return;
    if (!signer || !runtime.session) { if (runtime.unavailableReason) console.error('[sign-in] wallet not ready:', runtime.unavailableReason); setError(WALLET_NOT_READY); return; }
    operation.current = true;
    const version = epoch.current;
    setPending('sign'); setError(null);
    clearAccountSession();
    const expectedEpoch = accountEpoch();
    const abort = new AbortController();
    const unsubscribe = subscribeAccount(() => { if (accountEpoch() !== expectedEpoch) abort.abort(); });
    try {
      const account = getAddress(address);
      const challenge = await loginChallenge(account, abort.signal);
      const login = new WalletLogin(runtime.sessions, signer);
      const proof = await login.sign(challenge, window.location.origin, LOGIN_STATEMENT, consent => new Promise<boolean>(resolve => { consentResolver.current = resolve; setReview(consent); }));
      const kind = await signer.proofKind(account);
      const result = await login.verify(proof, (signed, signal) => loginProof(signed.challengeId, account, signed.signature, kind, signal));
      if (result.data.session.wallet.toLowerCase() !== account.toLowerCase()) throw new Error('The signed-in wallet does not match the connected account.');
      installAccountSession(result.data.session, result.data.accessToken, expectedEpoch, signer);
    } catch (failure) {
      if (failure instanceof Error && failure.message !== 'Login consent declined') console.error('[sign-in] sign-in failed:', failure);
      if (version === epoch.current) setError(failure instanceof Error && failure.message === 'Login consent declined' ? 'Sign-in cancelled' : "Couldn't sign in. Try again");
    } finally {
      unsubscribe(); operation.current = false; setPending(null); setReview(null); consentResolver.current = null;
    }
  };

  const approve = (value: boolean) => { consentResolver.current?.(value); consentResolver.current = null; setReview(null); };

  const connectedRow = address ? (
    <SignInWalletRow name={activeConnector ? displayName(activeConnector) : 'Connected wallet'} icon={activeConnector?.icon} detail={shortAddress(address, 6, 4)}
      trailing={<Button variant="ghost" size="sm" disabled={busy} onClick={() => { void changeWallet(); }}>Change</Button>} />
  ) : null;

  return (
    <ModalFrame open={open} onClose={guardedClose} size="sm" title={step === 'sign' ? 'Sign in with your wallet' : step === 'network' ? 'Switch network' : 'Sign in'}
      subtitle={step === 'connect' ? (embeddedWalletConfigured ? 'Connect a wallet or continue with email' : 'Connect a wallet to sign in') : step === 'network' ? 'Your wallet is on another network' : 'Sign one free message to log in. It is not a transaction'}
      closeLabel="Close sign-in">
      <div className="flex flex-col gap-3" aria-busy={busy || undefined}>
        {reason ? <NoticeBar tone="info" surface={2}>{reason}</NoticeBar> : null}

        {step === 'connect' ? (
          <>
            <EmbeddedSignIn disabled={busy} />
            <div className="flex flex-col gap-2" role="list" aria-label="Wallets">
              {primary.map(({ connector, badge }) => (
                <div role="listitem" key={connector.uid}>
                  <SignInWalletRow name={displayName(connector)} icon={connector.icon} badge={badge} busy={pending === connector.uid} disabled={probing(connector) || (busy && pending !== connector.uid)}
                    detail={connector.type === 'walletConnect' ? 'Scan with a mobile wallet' : undefined} onSelect={() => { void connectWallet(connector); }} />
                </div>
              ))}
              {extra.length ? (
                <>
                  <MoreWalletsRow open={showMore} count={extra.length} onToggle={() => setShowMore(value => !value)} controls={moreId} />
                  <div id={moreId} hidden={!showMore} className="flex flex-col gap-2">
                    {extra.map(({ connector, badge }) => (
                      <div role="listitem" key={connector.uid}>
                        <SignInWalletRow name={displayName(connector)} icon={connector.icon} badge={badge} busy={pending === connector.uid} disabled={probing(connector) || (busy && pending !== connector.uid)}
                          detail={connector.type === 'walletConnect' ? 'Scan with a mobile wallet' : undefined} onSelect={() => { void connectWallet(connector); }} />
                      </div>
                    ))}
                  </div>
                </>
              ) : null}
            </div>
            {noBrowserWallet ? <p role="note" className="text-sm text-text-muted">{noBrowserWalletNote(embeddedOffered)}</p> : null}
            {choices.length === 0 && !noBrowserWallet ? <p role="status" className="text-sm text-text-muted">{embeddedWalletConfigured ? 'No browser wallet found. Continue with email above' : "No wallet found in this browser. Install one or open this page in your wallet's browser"}</p> : null}
            {!walletConnectConfigured ? <p className="text-xs text-text-faint">Mobile wallets are not supported yet. Use a browser wallet</p> : null}
          </>
        ) : null}

        {step === 'network' ? (
          <>
            {connectedRow}
            <Button variant="primary" size="lg" fullWidth loading={pending === 'network'} disabled={busy} onClick={() => { void switchNetwork(); }}>Switch to Paxeer</Button>
          </>
        ) : null}

        {step === 'sign' ? (
          <>
            {connectedRow}
            {review ? (
              <div className="flex flex-col gap-2 rounded-md bg-surface-3 p-3">
                <p className="text-sm text-text">Signing this message logs you in. It is free and not a transaction</p>
                <pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all rounded-sm bg-surface-1 p-2 font-mono-sl text-xs text-text-muted">{review.message}</pre>
                <div className="flex gap-2">
                  <Button variant="primary" fullWidth onClick={() => approve(true)}>Sign message</Button>
                  <Button variant="secondary" fullWidth onClick={() => approve(false)}>Cancel</Button>
                </div>
              </div>
            ) : (
              <Button variant="primary" size="lg" fullWidth loading={pending === 'sign'} disabled={busy} onClick={() => { void signIn(); }}
                icon={activeConnector ? <WalletMark icon={activeConnector.icon} name={displayName(activeConnector)} size="sm" /> : undefined}>
                Sign in with {activeConnector ? displayName(activeConnector) : 'wallet'}
              </Button>
            )}
            {!runtime.wallet && runtime.unavailableReason ? <p role="alert" className="text-xs text-danger">{WALLET_NOT_READY}</p> : null}
          </>
        ) : null}

        {busy && !review ? <p role="status" className="text-xs text-text-muted">Waiting for your wallet</p> : null}
        {error ? <p role="alert" className="text-sm text-danger">{error}</p> : null}
      </div>
    </ModalFrame>
  );
}
