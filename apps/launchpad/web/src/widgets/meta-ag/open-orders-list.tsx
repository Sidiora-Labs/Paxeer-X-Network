'use client';
import { useEffect, useRef, useState } from 'react';
import { ListOrdered } from 'lucide-react';
import { EmptyState } from '@/ui/primitives';
import { reportUserFacingError } from '@/core/user-facing-error';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { eligibility, reconcileOrder } from './domain';
import type { AdvancedProvider, ConnectedOrder } from './advanced-provider';
export interface OpenOrdersListProps { refreshSignal?: number; provider?: AdvancedProvider }
export default function OpenOrdersList(props: OpenOrdersListProps) {
  if (!props.provider) return <p role="status" className="p-4 text-2xs text-text">Orders are not available yet</p>;
  return <ConnectedOrders {...props} provider={props.provider} />;
}
function ConnectedOrders({ provider, refreshSignal }: OpenOrdersListProps & { provider: AdvancedProvider }) {
  const [items, setItems] = useState<ConnectedOrder[]>([]), [cursor, setCursor] = useState<string | null>(null);
  const [message, setMessage] = useState(''), [busy, setBusy] = useState(false), [owner, setOwner] = useState(''), [ownerChecked, setOwnerChecked] = useState(false), [loaded, setLoaded] = useState(false);
  const generation = useRef(0), activeCancel = useRef(false);
  const [pending, setPending] = useState<Record<string, string>>({});
  useEffect(() => {
    let live = true;
    const check = () => { void provider.owner().then(account => { if (live) { setOwner(account); setOwnerChecked(true); } }).catch(() => { if (live) { setOwner(''); setOwnerChecked(true); } }); };
    check(); const timer = setInterval(check, 2000); return () => { live = false; clearInterval(timer); generation.current++; };
  }, [provider]);
  const load = async (after: string | null) => {
    const version = ++generation.current; setBusy(true);
    try {
      const page = await provider.orderPage(after);
      const recovered = await Promise.all(page.items.map(async item => ({ key: `${item.family}:${item.orderId}`, attempt: await provider.pendingCancel(item) })));
      if (version !== generation.current) return;
      setPending(previous => ({ ...(after ? previous : {}), ...Object.fromEntries(recovered.flatMap(value => value.attempt ? [[value.key, value.attempt.txHash]] : [])) }));
      setItems(previous => {
        if (!after) return page.items;
        const byId = new Map(previous.map(value => [`${value.family}:${value.orderId}`, value]));
        for (const value of page.items) {
          const key = `${value.family}:${value.orderId}`, current = byId.get(key);
          if (current) { const reconciled = reconcileOrder(current, { ...value, emitter: provider.manifest.orders!.address, committed: true, transferVerified: value.filledRaw > 0n || value.refundRaw > 0n }, provider.manifest.orders!.address); if (reconciled === current) continue; }
          byId.set(key, value);
        }
        return [...byId.values()];
      });
      setCursor(page.nextCursor); setMessage(''); setLoaded(true);
    } catch (error) { if (version === generation.current) setMessage(reportUserFacingError(error, "Couldn't load your orders, try again", 'meta-ag orders')); }
    finally { if (version === generation.current) setBusy(false); }
  };
  useEffect(() => { generation.current++; setItems([]); setPending({}); setCursor(null); setMessage(''); setLoaded(false); if (owner) void load(null); }, [provider, owner, refreshSignal]);
  const cancel = async (item: ConnectedOrder) => {
    if (activeCancel.current) return; activeCancel.current = true;
    const version = generation.current; setBusy(true);
    try {
      const pending = await provider.pendingCancel(item);
      const attempt = pending ?? await provider.cancel(item);
      if (version === generation.current) setPending(values => ({ ...values, [`${item.family}:${item.orderId}`]: attempt.txHash }));
      if (version === generation.current) setMessage('Cancel sent, checking...');
      const result = await provider.recoverCancel(item);
      if (version !== generation.current) return;
      if (result.outcome.state !== 'checking') setPending(values => { const next = { ...values }; delete next[`${item.family}:${item.orderId}`]; return next; });
      setItems(previous => previous.map(value => value.family === item.family && value.orderId === item.orderId ? result.order : value));
      setMessage(result.outcome.state === 'filled_race' ? 'Your order traded before the cancel went through' : result.outcome.state === 'cancelled' ? `Order cancelled. Refunded ${result.outcome.refundRaw} units` : result.outcome.state === 'checking' ? 'Still checking your cancel...' : 'Cancel failed, try again');
    } catch (error) { if (version === generation.current) setMessage(reportUserFacingError(error, 'Cancel failed, try again', 'meta-ag cancel order')); }
    finally { activeCancel.current = false; if (version === generation.current) setBusy(false); }
  };
  const blocker = provider.blockers.get('orders');
  if (blocker) return <p role="status">Orders are not available yet</p>;
  return <section className="rounded-2xl bg-surface-1 p-4 space-y-3"><h2 className="font-bold">Your orders</h2><button disabled={busy || !owner} onClick={() => void load(null)}>Refresh</button>{items.length === 0 && (ownerChecked && !owner ? <SignedOutState icon={ListOrdered} title="Sign in to see your orders" body="Connect the wallet you placed them with" /> : loaded ? <EmptyState icon={ListOrdered} title="No open orders" body="Limit and stop orders you place show up here" announce /> : message ? null : <p role="status">Loading orders...</p>)}{items.map(item => {
    const health = eligibility(item.eligible, item.keeperCheckedAt, item.keeperHealthy, Date.now());
    return <article key={`${owner}:${item.family}:${item.orderId}`} className="rounded-xl bg-surface-2 p-3 space-y-2"><p>Order {item.orderId} · {item.type} · {item.state}</p><p>Trigger: {health.eligibility === 'eligible' ? 'hit' : health.eligibility === 'ineligible' ? 'not hit yet' : 'checking'} · Order bot: {health.executionHealth === 'healthy' ? 'running' : 'slow'} · Checked {item.keeperCheckedAt ? new Date(item.keeperCheckedAt).toLocaleTimeString() : 'not yet'}</p><p>Traded {item.filledRaw.toString()} units · Refunded {item.refundRaw.toString()} units</p>{item.executionTxHash && <p className="break-all">Trade transaction {item.executionTxHash}</p>}{pending[`${item.family}:${item.orderId}`] && <p>Cancelling · {pending[`${item.family}:${item.orderId}`]}</p>}{(['active','triggered'].includes(item.state) || pending[`${item.family}:${item.orderId}`]) && <button disabled={busy} onClick={() => { if (window.confirm(`Cancel order ${item.orderId}? It may trade before the cancel goes through.`)) void cancel(item); }}>{pending[`${item.family}:${item.orderId}`] ? 'Check cancel status' : 'Cancel order'}</button>}</article>;
  })}{cursor && <button disabled={busy} onClick={() => void load(cursor)}>Load more</button>}<p role="status" className="break-all">{message}</p></section>;
}
