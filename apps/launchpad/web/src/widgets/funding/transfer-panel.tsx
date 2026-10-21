'use client';

import { useEffect, useMemo, useState } from 'react';
import { formatUnits } from 'viem';
import { useWalletRuntime } from '../../core/wallet-sdk/runtime';
import { assetKey, formatAssetAmount, nativePAX, parseAssetAmount } from '../../core/wallet-sdk/assets';
import { checkedCommittedHead, WalletTransferHistoryV2, type WalletTransferRecoveryUpdateV2 } from '../../core/wallet-sdk/backend';
import { WalletTransfer, type TransferReview } from './transfer';

/** Plain wording for transfer failures; the raw reason is logged, never shown. */
function plainTransferError(failure: unknown): string {
  const text = failure instanceof Error ? failure.message : '';
  console.error('[withdraw] failed', failure);
  if (/insufficient pax/i.test(text)) return 'Not enough PAX to cover the network fee';
  if (/insufficient/i.test(text)) return 'Not enough balance';
  if (/recipient|address/i.test(text)) return 'Enter a valid recipient';
  if (/amount must be positive/i.test(text)) return 'Enter an amount';
  if (/declined|rejected|denied/i.test(text)) return 'Withdrawal cancelled';
  if (/expired|stale|changed|increased|stabilize/i.test(text)) return 'Price or balance changed, review again';
  return "Couldn't send, try again";
}

export default function TransferPanel() {
  const runtime = useWalletRuntime();
  const { session, authority, publicClient, wallet, assets } = runtime;
  const [selection, setSelection] = useState('125:native');
  const [recipient, setRecipient] = useState('');
  const [amount, setAmount] = useState('');
  const [review, setReview] = useState<Readonly<TransferReview>>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const [hash, setHash] = useState<string>();
  const [recovery, setRecovery] = useState<{ account: string; entries: WalletTransferRecoveryUpdateV2[] }>();
  const asset = assets.find(item => assetKey(item) === selection) ?? assets[0];
  const transfer = useMemo(() => session && wallet && authority && publicClient ? new WalletTransfer(publicClient, wallet, runtime.sessions, signal => checkedCommittedHead(publicClient, authority, signal)) : undefined, [session?.sessionEpoch, wallet, authority, publicClient, runtime.sessions]);
  const [history, setHistory] = useState<WalletTransferHistoryV2>();
  useEffect(() => {
    setReview(undefined); setPending(false); setHash(undefined); setError(undefined);
    if (!session || !authority || !publicClient) { setHistory(undefined); return; }
    const account = session.account;
    let owned: WalletTransferHistoryV2 | undefined;
    try {
      owned = new WalletTransferHistoryV2(window.localStorage, account, publicClient, authority);
      setHistory(owned);
      owned.resume(update => setRecovery(previous => ({ account, entries: [...(previous?.account === account ? previous.entries.filter(item => item.attempt.txHash !== update.attempt.txHash) : []), update].slice(-25) })));
    } catch (failure) { setHistory(undefined); setError(plainTransferError(failure)); }
    return () => { owned?.close(); setHistory(undefined); };
  }, [session?.sessionEpoch, authority, publicClient]);
  useEffect(() => {
    if (!review || hash) return;
    const timeout = setTimeout(() => setReview(undefined), Math.max(0, review.expiresAt - Date.now()));
    return () => clearTimeout(timeout);
  }, [review, hash]);
  const maximum = async () => {
    if (!session || !publicClient || !authority || asset?.kind !== 'native' || pending) return;
    const scope = runtime.sessions.capture(); setPending(true); setError(undefined); setReview(undefined);
    try {
      if (!transfer) throw new Error('Transfer authority unavailable');
      const maximumRaw = await transfer.maximum(recipient);
      runtime.sessions.assertCurrent(scope, true);
      transfer.resetDraft(); setAmount(formatUnits(maximumRaw, 18));
    } catch (failure) { if (!scope.signal.aborted) setError(plainTransferError(failure)); }
    finally { if (!scope.signal.aborted) setPending(false); }
  };
  const edit = (set: (value: string) => void, value: string) => {
    if (pending || transfer?.hash) return;
    transfer?.resetDraft(); setReview(undefined); setError(undefined); set(value);
  };
  const preview = async () => {
    if (!transfer || !asset || !session || pending) return;
    const scope = runtime.sessions.capture();
    setPending(true); setError(undefined);
    try { const value = await transfer.preview(crypto.randomUUID(), asset, recipient, parseAssetAmount(asset, amount).toString()); runtime.sessions.assertCurrent(scope, true); setReview(value); }
    catch (failure) { if (!scope.signal.aborted) setError(plainTransferError(failure)); }
    finally { if (!scope.signal.aborted) setPending(false); }
  };
  const submit = async () => {
    if (!transfer || !review || !history || pending) return;
    if (recipient.trim().toLowerCase() !== review.recipient.toLowerCase() || !asset || assetKey(asset) !== assetKey(review.assetRef) || parseAssetAmount(asset, amount).toString() !== review.amountRaw) { setReview(undefined); setError('Details changed, review again'); return; }
    const submittedReview = review;
    setPending(true); setError(undefined);
    try {
      const value = await transfer.submit(submittedReview, async () => true);
      history.record(submittedReview, value);
      if (!submittedReview.scope.signal.aborted) {
        setHash(value);
        history.resume(update => setRecovery(previous => ({ account: submittedReview.wallet, entries: [...(previous?.account === submittedReview.wallet ? previous.entries.filter(item => item.attempt.txHash !== update.attempt.txHash) : []), update].slice(-25) })));
      }
    } catch (failure) {
      if (!submittedReview.scope.signal.aborted) {
        if (transfer.hash) setHash(transfer.hash);
        setError(plainTransferError(failure));
        if (!transfer.hash) setReview(undefined);
      }
    } finally { if (!submittedReview.scope.signal.aborted) setPending(false); }
  };
  if (!session || session.chainId !== 125) return <p role="status">Connect wallet on Paxeer to withdraw</p>;
  if (!transfer || !asset || !authority) return <p role="status">Withdrawals are not available yet</p>;
  const currentReview = review?.scope.sessionEpoch === session.sessionEpoch ? review : undefined;
  return <section aria-label="Withdraw" className="space-y-3 text-xs text-text">
    <h3 className="font-bold">Withdraw</h3><p>Send PAX or USDL to another wallet on Paxeer.</p>
    <label className="block">Coin<select disabled={pending || Boolean(hash)} className="mt-1 w-full rounded-lg bg-surface-2 p-2" value={assetKey(asset)} onChange={event => edit(setSelection, event.target.value)}>{assets.map(item => <option key={assetKey(item)} value={assetKey(item)}>{item.symbol}</option>)}</select></label>
    <label className="block">Recipient<input disabled={pending || Boolean(hash)} autoComplete="off" className="mt-1 w-full rounded-lg bg-surface-2 p-2" value={recipient} onChange={event => edit(setRecipient, event.target.value)} /></label>
    <label className="block">Amount<input disabled={pending || Boolean(hash)} inputMode="decimal" className="mt-1 w-full rounded-lg bg-surface-2 p-2" value={amount} onChange={event => edit(setAmount, event.target.value)} /></label>
    {asset.kind === 'native' && !hash && <button disabled={pending} className="rounded-lg bg-surface-2 px-3 py-2 disabled:opacity-50" onClick={() => { void maximum(); }}>Max</button>}
    {!hash && <button disabled={pending || !wallet || !history} className="rounded-lg bg-surface-2 px-3 py-2 disabled:opacity-50" onClick={() => { void preview(); }}>Review</button>}
    {!history && <p role="alert">Withdrawals are off on this device because browser storage is blocked.</p>}
    {currentReview && !hash && <div className="space-y-2 rounded-lg bg-surface-2 p-3"><h4>Confirm withdrawal</h4><p className="break-all">From: {currentReview.wallet}</p><p className="break-all">Recipient: {currentReview.recipient}</p><p>Network: Paxeer</p><p>Amount: {formatAssetAmount(currentReview.assetRef, currentReview.amountRaw)}</p><p>Network fee: {formatAssetAmount(nativePAX, currentReview.gasEstimateRaw)}</p><p className="text-2xs">Double-check the recipient. Your wallet will ask you to approve.</p><button disabled={pending || Date.now() >= currentReview.expiresAt} className="rounded-lg bg-accent px-3 py-2 text-on-accent disabled:opacity-50" onClick={() => { void submit(); }}>Approve transfer in wallet</button></div>}
    {hash && <div className="space-y-2"><p role="status" className="break-all">Sent! Waiting for confirmation. <a className="underline" target="_blank" rel="noreferrer" href={`${authority.network.explorerBase.replace(/\/$/, '')}/tx/${hash}`}>{hash}</a></p>{currentReview && <p className="break-all">Sending {formatAssetAmount(currentReview.assetRef, currentReview.amountRaw)} to {currentReview.recipient}</p>}</div>}
    {recovery?.account === session.account && <ul aria-label="Withdrawals" className="space-y-2">{recovery.entries.map(entry => <li className="break-all rounded-lg bg-surface-2 p-2" key={entry.attempt.txHash}><a href={`${authority.network.explorerBase.replace(/\/$/, '')}/tx/${entry.attempt.txHash}`} target="_blank" rel="noreferrer">{entry.attempt.txHash}</a><p>{entry.state === 'confirmed_success' ? 'Confirmed' : entry.state === 'confirmed_reverted' ? 'Failed, no funds moved' : entry.error ? "Can't confirm yet, check the explorer" : 'Pending'}</p>{entry.state === 'confirmed_success' && entry.confirmedTransfer && <p>Sent {formatAssetAmount(entry.confirmedTransfer.asset, entry.confirmedTransfer.amountRaw)} to {entry.confirmedTransfer.recipient}</p>}</li>)}</ul>}
    {pending && <p role="status">Check your wallet...</p>}{error && <p role="alert">{error}</p>}
  </section>;
}
