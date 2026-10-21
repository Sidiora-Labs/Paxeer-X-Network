'use client';
import { useEffect, useRef, useState } from 'react';
import { address, exactUnits, type OrderKind } from './domain';
import type { AdvancedProvider, PreparedOrder } from './advanced-provider';
import { loadMetaAgTokens, type MetaAgToken } from './token-universe';
import { reportUserFacingError } from '@/core/user-facing-error';

export interface OrderFormProps { onPlaced?: () => void; provider?: AdvancedProvider }
export default function OrderForm(props: OrderFormProps) {
  if (!props.provider) return <p role="status" className="p-4 text-2xs text-text">Orders are not available yet</p>;
  return <ConnectedOrderForm {...props} provider={props.provider} />;
}
function ConnectedOrderForm({ provider, onPlaced }: OrderFormProps & { provider: AdvancedProvider }) {
  const [tokens, setTokens] = useState<MetaAgToken[]>([]);
  const [token, setToken] = useState('');
  const [kind, setKind] = useState<OrderKind>(provider.manifest.supportedKinds[0]);
  const [amount, setAmount] = useState('');
  const [trigger, setTrigger] = useState('');
  const [limit, setLimit] = useState('');
  const [minimum, setMinimum] = useState('');
  const [expiry, setExpiry] = useState('86400');
  const [review, setReview] = useState<PreparedOrder | null>(null);
  const [owner, setOwner] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const generation = useRef(0), inFlight = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    void loadMetaAgTokens(controller.signal).then(values => { if (!controller.signal.aborted) setTokens(values.filter(value => value.tokenAddress.toLowerCase() !== provider.manifest.usdl.toLowerCase())); }).catch(error => { if (!controller.signal.aborted) { console.error(error); setMessage("Couldn't load coins, try again"); } });
    let alive = true;
    const check = () => { void provider.owner().then(value => { if (alive) setOwner(value); }).catch(() => { if (alive) setOwner(''); }); };
    check(); const timer = setInterval(check, 2000);
    return () => { generation.current++; alive = false; controller.abort(); clearInterval(timer); };
  }, [provider]);
  useEffect(() => { generation.current++; setReview(null); setMessage(''); }, [provider, owner, token, kind, amount, trigger, limit, minimum, expiry]);
  const prepare = async () => {
    if (inFlight.current) return;
    inFlight.current = true; const version = generation.current; setBusy(true); setMessage('');
    try {
      const account = await provider.owner(), tokenAddress = address(token);
      const seconds = BigInt(expiry);
      const expiresAt = seconds === 0n && provider.manifest.gtcSentinel !== undefined ? BigInt(provider.manifest.gtcSentinel) : BigInt(Math.floor(Date.now() / 1000)) + seconds;
      const buy = kind === 'limit-buy' || kind === 'stop-limit-buy';
      const outputDecimals = await provider.decimals(buy ? tokenAddress : provider.manifest.usdl);
      const prepared = await provider.prepareOrder({ kind, owner: account, token: tokenAddress, settlementAsset: provider.manifest.usdl, amount, triggerPrice: kind.startsWith('stop') ? trigger : undefined, limitPrice: kind === 'stop-loss' ? undefined : limit, expiresAt }, exactUnits(minimum, outputDecimals));
      if (await provider.owner() !== account) throw new Error('Your wallet changed, review again');
      if (version === generation.current) setReview(prepared);
    } catch (error) { if (version === generation.current) setMessage(reportUserFacingError(error, "Couldn't prepare your order, try again", 'meta-ag prepare order')); }
    finally { inFlight.current = false; setBusy(false); }
  };
  const place = async () => {
    if (!review || busy || inFlight.current) return;
    inFlight.current = true; const version = generation.current;
    setBusy(true); setMessage('Confirm in your wallet...');
    try { const result = await provider.placeOrder(review); if (version !== generation.current) return; setMessage(`Order ${result.orderId} placed! It trades when the price hits your trigger`); setReview(null); onPlaced?.(); }
    catch (error) { if (version === generation.current) { setMessage(reportUserFacingError(error, "Couldn't place your order, try again", 'meta-ag place order')); setReview(null); } }
    finally { inFlight.current = false; setBusy(false); }
  };
  const blocker = provider.blockers.get('orders') ?? provider.blockers.get('oracle');
  if (blocker) return <p role="status" className="p-4 text-2xs text-text">Orders are not available yet</p>;
  return <section className="rounded-2xl bg-surface-1 p-4 space-y-3">
    <h2 className="text-base font-bold">Limit and stop orders</h2>
    <p className="text-2xs text-text">Prices are in {provider.manifest.priceDenomination}. Hitting your trigger doesn't guarantee that exact price</p>
    <label className="block">Order type<select className="block w-full bg-surface-2 p-3 rounded-xl" value={kind} onChange={event => setKind(event.target.value as OrderKind)}>{provider.manifest.supportedKinds.map(value => <option key={value} value={value}>{value.replaceAll('-', ' ')}</option>)}</select></label>
    <label className="block">Coin<select className="block w-full bg-surface-2 p-3 rounded-xl" value={token} onChange={event => setToken(event.target.value)}><option value="">Select coin</option>{tokens.map(value => <option key={value.tokenAddress} value={value.tokenAddress}>{value.symbol} · {value.tokenAddress}</option>)}</select></label>
    <label className="block">Amount<input className="block w-full bg-surface-2 p-3 rounded-xl" inputMode="decimal" value={amount} onChange={event => setAmount(event.target.value)} /></label>
    {kind.startsWith('stop') && <label className="block">Trigger price<input className="block w-full bg-surface-2 p-3 rounded-xl" inputMode="decimal" value={trigger} onChange={event => setTrigger(event.target.value)} /></label>}
    {kind !== 'stop-loss' && <label className="block">Limit price<input className="block w-full bg-surface-2 p-3 rounded-xl" inputMode="decimal" value={limit} onChange={event => setLimit(event.target.value)} /></label>}
    <label className="block">Minimum received<input className="block w-full bg-surface-2 p-3 rounded-xl" inputMode="decimal" value={minimum} onChange={event => setMinimum(event.target.value)} /></label>
    <label className="block">Expires in<select className="block w-full bg-surface-2 p-3 rounded-xl" value={expiry} onChange={event => setExpiry(event.target.value)}>{[3600,86400,604800].filter(value => BigInt(value) <= BigInt(provider.manifest.maximumOrderLifetimeSeconds)).map(value => <option key={value} value={value}>{value / 3600 === 1 ? '1 hour' : value / 3600 === 168 ? '7 days' : `${value / 3600} hours`}</option>)}{provider.manifest.gtcSentinel !== undefined && <option value="0">Good until cancelled</option>}</select></label>
    <button className="rounded-xl bg-accent text-on-accent p-3 disabled:opacity-40" disabled={busy || !owner} onClick={() => void prepare()}>Review order</button>
    {review && <div className="rounded-xl bg-surface-2 p-3 space-y-2"><p>Amount {review.intent.amountRaw.toString()} units of {review.escrowAsset}</p><p>Minimum received {review.minimumOutput.toString()} units</p><p>Trigger {review.intent.triggerPriceRaw?.toString() ?? 'None'} · Limit {review.intent.limitPriceRaw?.toString() ?? 'None'}</p><p>Expires {review.intent.expiresAt === BigInt(provider.manifest.gtcSentinel ?? '-1') ? 'when cancelled' : new Date(Number(review.intent.expiresAt) * 1000).toLocaleString()}</p><p>{review.fees}</p><p>{review.cancellation}</p><p>Approves {review.spender}</p><button disabled={busy} className="rounded-xl bg-info text-on-info p-3 disabled:opacity-40" onClick={() => void place()}>Place order</button></div>}
    <p role="status" className="text-2xs break-all">{message}</p>
  </section>;
}
