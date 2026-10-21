'use client';
import { useEffect, useRef, useState } from 'react';
import { address, exactUnits, routeNetOutput, type RouteQuote } from './domain';
import type { AdvancedProvider } from './advanced-provider';
import { loadMetaAgTokens, type MetaAgToken } from './token-universe';
import { reportUserFacingError } from '@/core/user-facing-error';
export default function BestRouteSwapPanel({ provider }: { provider?: AdvancedProvider } = {}) {
  if (!provider) return <p role="status" className="p-4 text-2xs text-text">Swaps are not available yet</p>;
  return <ConnectedRoute provider={provider} />;
}
function ConnectedRoute({ provider }: { provider: AdvancedProvider }) {
  const [tokens, setTokens] = useState<MetaAgToken[]>([]), [input, setInput] = useState(''), [output, setOutput] = useState(''), [amount, setAmount] = useState(''), [minimum, setMinimum] = useState('');
  const [quote, setQuote] = useState<RouteQuote | null>(null), [reviewed, setReviewed] = useState(false), [message, setMessage] = useState(''), [busy, setBusy] = useState(false), [account, setAccount] = useState('');
  const generation = useRef(0);
  useEffect(() => { const controller = new AbortController(); void loadMetaAgTokens(controller.signal).then(setTokens).catch(error => { console.error(error); setMessage("Couldn't load coins, try again"); }); let live = true; const check = () => { void provider.owner().then(value => { if (live) setAccount(value); }).catch(() => { if (live) setAccount(''); }); }; check(); const interval = setInterval(check, 2000); return () => { live = false; clearInterval(interval); controller.abort(); generation.current++; }; }, [provider]);
  useEffect(() => {
    const version = ++generation.current; setQuote(null); setReviewed(false); setMessage('');
    setBusy(false);
    if (!input || !output || !amount || !account) return;
    const timer = setTimeout(() => { setBusy(true); void provider.quote(address(input), address(output), amount).then(value => { if (version === generation.current) { setQuote(value); if (!value && provider.adapterDiagnostics.size > 0) console.error([...provider.adapterDiagnostics]); setMessage(value ? '' : 'No route found for this pair'); } }).catch(error => { if (version === generation.current) setMessage(reportUserFacingError(error, "Couldn't get a price, try again", 'meta-ag quote')); }).finally(() => { if (version === generation.current) setBusy(false); }); }, 300);
    return () => clearTimeout(timer);
  }, [provider, input, output, amount, account]);
  useEffect(() => { setReviewed(false); }, [minimum]);
  const execute = async () => {
    if (!quote || !reviewed || busy) return; const version = generation.current; setBusy(true);
    try { const decimals = await provider.decimals(quote.tokenOut), raw = exactUnits(minimum, decimals); const result = await provider.executeRoute(quote, raw, BigInt(Math.floor(Date.now() / 1000)) + 300n); if (version === generation.current) { setMessage(`Swapped! Transaction ${result.transactionHash}`); setQuote(null); setReviewed(false); } }
    catch (error) { if (version === generation.current) { setMessage(reportUserFacingError(error, 'Swap failed, try again', 'meta-ag swap')); setQuote(null); setReviewed(false); } }
    finally { if (version === generation.current) setBusy(false); }
  };
  const blocker = provider.blockers.get('routes'); if (blocker) return <p role="status">Swaps are not available yet</p>;
  return <section className="rounded-2xl bg-surface-1 p-4 space-y-3"><h2 className="font-bold">Swap</h2>{(['input','output'] as const).map(side => <label className="block" key={side}>{side === 'input' ? 'You pay' : 'You receive'}<select className="block w-full bg-surface-2 rounded-xl p-3" value={side === 'input' ? input : output} onChange={event => side === 'input' ? setInput(event.target.value) : setOutput(event.target.value)}><option value="">Select coin</option>{tokens.map(token => <option key={token.tokenAddress} value={token.tokenAddress}>{token.symbol} · {token.tokenAddress}</option>)}</select></label>)}<label className="block">Amount<input className="block w-full bg-surface-2 rounded-xl p-3" inputMode="decimal" value={amount} onChange={event => setAmount(event.target.value)} /></label><label className="block">Minimum received<input className="block w-full bg-surface-2 rounded-xl p-3" inputMode="decimal" value={minimum} onChange={event => setMinimum(event.target.value)} /></label>{quote && <div className="rounded-xl p-3 space-y-2"><p>Route {quote.adapterId}</p><p>You receive {routeNetOutput(quote).toString()} units</p><p>Fee {quote.feeRaw.toString()} units of {quote.feeAsset}</p><p>You pay {quote.amountInRaw.toString()} units of {quote.tokenIn}</p><p>To {quote.recipient}</p><p>Price good until {new Date(quote.expiresAt).toLocaleTimeString()}</p><label><input type="checkbox" checked={reviewed} onChange={event => setReviewed(event.target.checked)} /> I checked the coins, amounts and minimum received</label><button className="block rounded-xl bg-accent text-on-accent p-3 disabled:opacity-40" disabled={busy || !reviewed || !minimum || Date.now() >= quote.expiresAt} onClick={() => void execute()}>Swap</button></div>}<p role="status" className="break-all">{busy ? 'Finding the best price...' : message}</p></section>;
}
