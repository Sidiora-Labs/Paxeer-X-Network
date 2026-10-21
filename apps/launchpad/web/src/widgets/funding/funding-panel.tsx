'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { getAddress, type Hex } from 'viem';
import { useWalletRuntime } from '../../core/wallet-sdk/runtime';
import { assetKey, formatAssetAmount, parseAssetAmount } from '../../core/wallet-sdk/assets';
import { checkedCommittedHead } from '../../core/wallet-sdk/backend';
import { depositIntent, fundingURI, FundingTracker, type FundingSnapshot } from './funding';
import { fiatFundingAvailability } from './vendor-gate';

const depositStateLabel: Record<FundingSnapshot['state'], string> = { instructions: 'Waiting for deposit', checking_paused: 'Check paused, tap refresh', detected: 'Deposit seen, confirming...', balance_confirmed: 'Deposit received', reverted: 'Deposit failed', unknown_external_source: 'No deposit to this wallet found' };

export default function FundingPanel() {
  const runtime = useWalletRuntime();
  const { session, authority, publicClient, assets } = runtime;
  const [selected, setSelected] = useState('125:native');
  const [amount, setAmount] = useState('');
  const [error, setError] = useState<string>();
  const [copied, setCopied] = useState<string>();
  const [busy, setBusy] = useState(false);
  const refreshing = useRef<{ epoch: number; queued: Hex[] } | undefined>(undefined);
  const [observed, setObserved] = useState<{ epoch: number; balances: Record<string, FundingSnapshot>; history: FundingSnapshot[] }>({ epoch: -1, balances: {}, history: [] });
  const asset = assets.find(item => assetKey(item) === selected) ?? assets[0];
  useEffect(() => {
    try {
      const raw = sessionStorage.getItem('kindle:funding-intent:v2');
      if (!raw) return;
      const saved: unknown = JSON.parse(raw);
      if (typeof saved === 'object' && saved !== null && 'asset' in saved && typeof saved.asset === 'string' && 'amount' in saved && typeof saved.amount === 'string' && saved.amount.length <= 100) { setSelected(saved.asset); setAmount(saved.amount); }
    } catch { setError("Couldn't restore your last deposit details"); }
  }, []);
  useEffect(() => { try { sessionStorage.setItem('kindle:funding-intent:v2', JSON.stringify({ asset: selected, amount })); } catch { setError("Couldn't save your deposit details on this device"); } }, [selected, amount]);
  const trackers = useMemo(() => {
    if (!session || session.chainId !== 125 || !authority || !publicClient) return [];
    return assets.map(item => new FundingTracker(publicClient, runtime.sessions, depositIntent(session.account, item), signal => checkedCommittedHead(publicClient, authority, signal), detected => {
      if (runtime.sessions.sessionEpoch !== session.sessionEpoch) return;
      setObserved(previous => ({ epoch: session.sessionEpoch, balances: { ...(previous.epoch === session.sessionEpoch ? previous.balances : {}), [assetKey(item)]: detected }, history: [...(previous.epoch === session.sessionEpoch ? previous.history : []), detected].slice(-25) }));
    }));
  }, [session?.sessionEpoch, authority, publicClient, assets, runtime.sessions]);
  const refresh = async (hash?: Hex, carried: readonly Hex[] = []) => {
    if (!session || !trackers.length) return;
    const scope = runtime.sessions.capture();
    if (refreshing.current?.epoch === scope.sessionEpoch) {
      if (hash && !refreshing.current.queued.some(item => item.toLowerCase() === hash.toLowerCase())) {
        if (refreshing.current.queued.length >= 25) setError('Too many deposits to check at once. Tap refresh balance');
        else refreshing.current.queued.push(hash);
      }
      return;
    }
    const flight = { epoch: scope.sessionEpoch, queued: [...carried] };
    refreshing.current = flight;
    setBusy(true); setError(undefined);
    try {
      const values = await Promise.all(trackers.map(tracker => tracker.refresh(hash)));
      runtime.sessions.assertCurrent(scope, true);
      setObserved(previous => ({ epoch: scope.sessionEpoch, balances: Object.fromEntries(trackers.map((tracker, index) => { const key = assetKey(tracker.intent.assetRef), value = values[index]!; return [key, value.state === 'unknown_external_source' && previous.epoch === scope.sessionEpoch && previous.balances[key] ? previous.balances[key] : value]; })), history: hash ? [...(previous.epoch === scope.sessionEpoch ? previous.history.filter(entry => entry.hash !== hash) : []), ...(values.every(value => value.state === 'unknown_external_source') ? [values[0]!] : values.filter(value => value.hash && value.state !== 'unknown_external_source'))].slice(-25) : previous.epoch === scope.sessionEpoch ? previous.history : [] }));
    } catch (failure) { if (!scope.signal.aborted) { console.error('[funding] balance check failed', failure); setError("Couldn't check your balance, try again"); } }
    finally {
      if (refreshing.current === flight) {
        refreshing.current = undefined;
        if (!scope.signal.aborted) { setBusy(false); const next = flight.queued.shift(); if (next) void refresh(next, flight.queued); }
      }
    }
  };
  useEffect(() => {
    setBusy(false); setError(undefined); setCopied(undefined);
    if (!session || !authority || !trackers.length) return;
    const initial = setTimeout(() => { void refresh(); }, 50);
    let last: Hex | undefined;
    const unsubscribe = authority.subscribeIncomingTransfer?.(session.account, hint => {
      if (hint.chainId !== 125 || !/^0x[0-9a-fA-F]{40}$/.test(hint.recipient) || getAddress(hint.recipient) !== session.account || !/^0x[0-9a-fA-F]{64}$/.test(hint.transactionHash) || hint.transactionHash.toLowerCase() === last?.toLowerCase()) return;
      last = hint.transactionHash;
      void refresh(hint.transactionHash);
    });
    return () => { clearTimeout(initial); unsubscribe?.(); };
  }, [session?.sessionEpoch, authority, trackers]);
  const copy = async (value: string, label: string) => {
    try { await navigator.clipboard.writeText(value); setCopied(label); setError(undefined); }
    catch { setError("Couldn't copy. Select the text and copy it"); }
  };
  if (!session) return <p role="status">Connect wallet to deposit</p>;
  if (session.chainId !== 125) return <p role="alert">Switch to Paxeer network to deposit</p>;
  if (!asset || !authority || !publicClient) return <p role="status">Deposits are not available yet</p>;
  let uri: string | undefined;
  try { uri = fundingURI(depositIntent(session.account, asset, amount === '' ? undefined : parseAssetAmount(asset, amount).toString())); }
  catch { /* The entered amount stays editable; invalid precision cannot enter a funding URI. */ }
  const QRCode = authority.FundingQRCode;
  const balances = observed.epoch === session.sessionEpoch ? observed.balances : {};
  return <section aria-label="Deposit" className="space-y-3 text-xs text-text">
    <h3 className="font-bold">Deposit PAX or USDL</h3>
    <p>On Paxeer network. PAX pays network fees, USDL is what you trade with.</p>
    <div className="grid grid-cols-2 gap-2">{assets.map(item => { const value = balances[assetKey(item)]; return <div key={assetKey(item)} className="rounded-lg bg-surface-2 p-3"><p>{item.symbol === 'PAX' ? 'PAX balance' : 'USDL balance'}</p><p className="mt-1">{value?.state === 'balance_confirmed' && value.balanceRaw !== undefined ? formatAssetAmount(item, value.balanceRaw) : '—'}</p>{value?.state === 'checking_paused' && <p role="status" className="text-2xs">Balance check paused, tap refresh</p>}</div>; })}</div>
    <label className="block">Coin<select className="mt-1 w-full rounded-lg bg-surface-2 p-2" value={assetKey(asset)} onChange={event => { setSelected(event.target.value); setAmount(''); }}>{assets.map(item => <option key={assetKey(item)} value={assetKey(item)}>{item.symbol === 'PAX' ? 'PAX (network fees)' : 'USDL (for trading)'}</option>)}</select></label>
    <label className="block">Amount (optional)<input inputMode="decimal" className="mt-1 w-full rounded-lg bg-surface-2 p-2" value={amount} onChange={event => setAmount(event.target.value)} /></label>
    {amount && !uri && <p role="alert">Enter an amount with up to {asset.decimals} decimals</p>}
    <p className="break-all">Your deposit address: {session.account}</p>
    <p className="break-all">{asset.kind === 'native' ? 'PAX is the network coin, no token contract' : `USDL token contract: ${asset.address}`}</p>
    <button className="rounded-lg bg-surface-2 px-3 py-2" onClick={() => { void copy(session.account, 'Address'); }}>Copy address</button>
    {uri && <div className="space-y-2"><button className="rounded-lg bg-surface-2 px-3 py-2" onClick={() => { void copy(uri!, 'Deposit link'); }}>Copy deposit link</button><a className="ml-2 underline" href={uri}>Open in wallet</a>{QRCode ? <QRCode value={uri} label={`Deposit ${asset.symbol} on Paxeer`} /> : <p role="status">QR code is not available. Copy the address instead</p>}</div>}
    {copied && <p role="status">{copied} copied</p>}
    <button disabled={busy} className="rounded-lg bg-accent px-3 py-2 text-on-accent disabled:opacity-50" onClick={() => { void refresh(); }}>{busy ? 'Checking...' : 'Refresh balance'}</button>
    <p className="text-2xs text-text-faint">Deposits show up once the network confirms them. Only send on Paxeer, funds sent on another network can't be recovered.</p>
    <p role="status" className="text-2xs">Buying with card is not available yet. {fiatFundingAvailability.alternative}</p>
    {observed.epoch === session.sessionEpoch && observed.history.length > 0 && <ul aria-label="Deposit history" className="space-y-2">{observed.history.map((entry, index) => <li key={`${entry.hash}:${index}`} className="break-all rounded-lg bg-surface-2 p-2"><a href={`${authority.network.explorerBase.replace(/\/$/, '')}/tx/${entry.hash}`} target="_blank" rel="noreferrer">{entry.hash}</a><p>{depositStateLabel[entry.state]}</p></li>)}</ul>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
