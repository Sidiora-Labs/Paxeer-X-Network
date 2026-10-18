'use client';
import { useEffect, useState, useSyncExternalStore } from 'react';
import { useAccount } from 'wagmi';
import { useQueryClient } from '@tanstack/react-query';
import { getAddress } from 'viem';
import { PaxeerWallet, connectorProvider } from '@/core/wallet-sdk/client';
import { validateLoginChallenge } from '@/core/wallet-sdk/auth';
import { accountEpoch, accountSession, registerAccountInvalidator, clearAccountSession, installAccountSession, loginChallenge, loginProof, signOutAccount, subscribeAccount } from './account-transport';
const serverSnapshot = () => null;
let activeAccountViews = 0;
export function useAccountSession() {
  const wallet = useAccount();
  useEffect(() => { activeAccountViews++; return () => { activeAccountViews--; if (activeAccountViews === 0) clearAccountSession(false); }; }, []);
  const queryClient = useQueryClient();
  registerAccountInvalidator(queryClient, () => {
    void queryClient.cancelQueries({ predicate: query => query.queryKey[0] === 'private-account' });
    queryClient.removeQueries({ predicate: query => query.queryKey[0] === 'private-account' });
  });
  const snapshot = useSyncExternalStore(subscribeAccount, accountSession, serverSnapshot);
  const [authenticating, setAuthenticating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const matches = !!snapshot && Date.parse(snapshot.session.accessExpiresAt) > Date.now() && !!wallet.address && wallet.isConnected && wallet.chainId === 125 && snapshot.session.wallet.toLowerCase() === wallet.address.toLowerCase();
  useEffect(() => { if (snapshot && !matches) clearAccountSession(); }, [snapshot, matches]);
  useEffect(() => {
    const clear = () => clearAccountSession();
    window.addEventListener('pagehide', clear);
    return () => window.removeEventListener('pagehide', clear);
  }, []);
  useEffect(() => {
    if (!snapshot || !wallet.connector) return;
    let disposed = false;
    let remove: (() => void) | undefined;
    void connectorProvider(wallet.connector).then(async provider => {
      const { isWalletProvider } = await import('@/core/wallet-sdk/client');
      if (disposed || !isWalletProvider(provider)) return;
      const changed = () => { if (accountSession() === snapshot) clearAccountSession(); };
      provider.on('accountsChanged', changed); provider.on('chainChanged', changed); provider.on('disconnect', changed);
      remove = () => { provider.removeListener('accountsChanged', changed); provider.removeListener('chainChanged', changed); provider.removeListener('disconnect', changed); };
    }).catch(() => { if (!disposed && accountSession() === snapshot) clearAccountSession(); });
    return () => { disposed = true; remove?.(); };
  }, [snapshot, wallet.connector]);
  async function authenticate() {
    if (authenticating) return;
    setError(null); setAuthenticating(true);
    clearAccountSession(); const expectedEpoch = accountEpoch(); const abort = new AbortController();
    const unsubscribe = subscribeAccount(() => { if (accountEpoch() !== expectedEpoch) abort.abort(); });
    try {
      if (!wallet.isConnected || !wallet.address || wallet.chainId !== 125 || !wallet.connector) throw new Error('Connect your wallet on Paxeer first');
      const account = getAddress(wallet.address);
      const provider = await connectorProvider(wallet.connector);
      const signer = new PaxeerWallet(provider);
      const changed = () => clearAccountSession();
      provider.on('accountsChanged', changed); provider.on('chainChanged', changed); provider.on('disconnect', changed);
      try {
        const challenge = await loginChallenge(account, abort.signal);
        validateLoginChallenge(challenge, window.location.origin, account, 'Sign in to Sidiora.');
        const kind = await signer.proofKind(account);
        const signature = await signer.signMessage(account, challenge.message);
        validateLoginChallenge(challenge, window.location.origin, account, 'Sign in to Sidiora.');
        if (abort.signal.aborted) throw new Error('Wallet changed during authentication');
        const response = await loginProof(challenge.challengeId, account, signature, kind, abort.signal);
        if (response.data.session.wallet.toLowerCase() !== account.toLowerCase()) throw new Error('Authentication wallet mismatch');
        installAccountSession(response.data.session, response.data.accessToken, expectedEpoch, signer);
      } finally { provider.removeListener('accountsChanged', changed); provider.removeListener('chainChanged', changed); provider.removeListener('disconnect', changed); }
    } catch (failure) {
      const text = failure instanceof Error ? failure.message : '';
      if (text !== 'Connect your wallet on Paxeer first') console.error('[account] sign-in failed', failure);
      setError(text === 'Connect your wallet on Paxeer first' ? text : /reject|denied|cancel/i.test(text) ? 'Sign-in cancelled' : "Couldn't sign in, try again");
    }
    finally { unsubscribe(); setAuthenticating(false); }
  }
  return { session: matches ? snapshot : null, authenticating, error, authenticate, signOut: signOutAccount };
}
