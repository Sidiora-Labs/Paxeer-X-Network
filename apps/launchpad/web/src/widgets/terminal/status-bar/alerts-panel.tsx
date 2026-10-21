'use client';
import { useId, useState, useSyncExternalStore } from 'react';
import { BellRing, Trash2 } from 'lucide-react';
import type { Operations, V2ContractTypes } from '@kindle/sdk';
import { useSessionOperations } from '@/core/market/operations';
import { isSignedOutError, toUnsupported, type Unsupported } from '@/core/market/unsupported';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { USDL_ADDRESS } from '@/core/network/contracts';
import { openAuthModal } from '@/shell/auth-modal-store';
import { Button, EmptyState, ErrorState, FormField, IconButton, SegmentedControl, UnavailableState } from '@/ui/primitives';
import { exactPriceRatio } from '@/page-modules/token/market-presentation';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { alertCooldowns, alertCreateBody, priceRatio, type AlertDirection } from './model';

type Alert = V2ContractTypes['Alert'];

/*
 * The contract has IF_ALERT_CREATE and IF_ALERT_DELETE but no list read, so the
 * panel can only show alerts this browser created during the current session
 * (each one is the server's own IF_ALERT_CREATE response).
 */
let created: readonly Alert[] = [];
const listeners = new Set<() => void>();
function setCreated(next: readonly Alert[]) { created = next; listeners.forEach(listener => listener()); }
function subscribeCreated(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; }
const readCreated = () => created;

export async function createAlert(operations: Operations, body: NonNullable<ReturnType<typeof alertCreateBody>>, idempotencyKey: string): Promise<Alert> {
  const response = await operations.call('IF_ALERT_CREATE', { body }, { headers: { 'Idempotency-Key': idempotencyKey } });
  const alert = response.data.alert;
  if (alert.poolAddress.toLowerCase() !== body.poolAddress) throw new Error('Alert response names a different pool');
  return alert;
}

export async function deleteAlert(operations: Operations, alertId: string): Promise<void> {
  await operations.call('IF_ALERT_DELETE', { path: { alertId } });
}

type Failure = { kind: 'unsupported'; marker: Unsupported } | { kind: 'error'; message: string };
function failure(operation: string, error: unknown): Failure {
  if (isSignedOutError(error)) return { kind: 'error', message: 'Your session ended. Sign in again to manage price alerts.' };
  const marker = toUnsupported(operation, error);
  return marker ? { kind: 'unsupported', marker } : { kind: 'error', message: 'Something went wrong, try again' };
}

function FailureNote({ value, title }: { value: Failure; title: string }) {
  return value.kind === 'unsupported'
    ? <UnavailableState variant="inline" title={title} operation={value.marker.operation} status={value.marker.status} />
    : <ErrorState variant="inline" message={value.message} />;
}

const directionOptions: { value: AlertDirection; label: string }[] = [
  { value: 'crosses_above', label: 'Rises above' },
  { value: 'crosses_below', label: 'Falls below' },
];
const cooldownOptions = alertCooldowns.map(seconds => ({ value: String(seconds), label: seconds < 3600 ? `${seconds / 60}m` : '1h' }));

function AlertForm({ pool, operations, wallet }: { pool: string; operations: Operations; wallet: string }) {
  const id = useId();
  const snapshot = usePoolSnapshot(pool);
  const [direction, setDirection] = useState<AlertDirection>('crosses_above');
  const [cooldown, setCooldown] = useState('300');
  const [price, setPrice] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Failure | null>(null);
  const token = snapshot.data?.token ?? null;
  const decimals = token && token.decimalsQuality === 'verified' ? token.decimals : null;
  const tokenAddress = snapshot.data?.pool.tokenAddress ?? null;
  const priceError = price.trim() !== '' && !priceRatio(price) ? 'Enter a USDL price above 0' : null;
  const blocked = snapshot.isPending ? 'Loading...' : snapshot.isError ? "This coin can't load right now" : decimals === null || tokenAddress === null ? 'Alerts are not available for this coin yet' : null;

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (busy || blocked || decimals === null || tokenAddress === null) return;
    const body = alertCreateBody({ poolAddress: pool, tokenAddress, tokenDecimals: decimals, usdlAddress: USDL_ADDRESS, direction, price, cooldownSeconds: Number(cooldown), observedAt: new Date() });
    if (!body) { setError({ kind: 'error', message: 'Enter a USDL price above 0' }); return; }
    setBusy(true); setError(null);
    try {
      const alert = await createAlert(operations, body, crypto.randomUUID());
      if (alert.wallet.toLowerCase() === wallet.toLowerCase()) setCreated([alert, ...readCreated().filter(row => row.alertId !== alert.alertId)]);
      setPrice('');
    } catch (cause) {
      setError(failure('IF_ALERT_CREATE', cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-2 px-2" aria-label="Create price alert">
      <SegmentedControl label="Alert direction" options={directionOptions} value={direction} onChange={setDirection} fullWidth />
      <FormField id={`${id}-price`} label="Price (USDL per token)" surface="surface-2" error={priceError} mono
        inputProps={{ inputMode: 'decimal', autoComplete: 'off', placeholder: '0.0001', value: price, onChange: event => setPrice(event.target.value) }} />
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-text-faint">Repeat at most every</span>
        <SegmentedControl label="Alert cooldown" options={cooldownOptions} value={cooldown} onChange={setCooldown} />
      </div>
      <Button type="submit" variant="primary" size="sm" loading={busy} disabled={Boolean(blocked) || price.trim() === '' || priceError !== null} title={blocked ?? undefined}>Create alert</Button>
      {blocked && !snapshot.isPending ? <p className="text-xs text-text-faint">{blocked}</p> : null}
      {error ? <FailureNote value={error} title="Price alerts" /> : null}
    </form>
  );
}

function AlertRow({ alert, operations }: { alert: Alert; operations: Operations }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Failure | null>(null);
  async function remove() {
    setBusy(true); setError(null);
    try {
      await deleteAlert(operations, alert.alertId);
      setCreated(readCreated().filter(row => row.alertId !== alert.alertId));
    } catch (cause) {
      setError(failure('IF_ALERT_DELETE', cause));
      setBusy(false);
    }
  }
  return (
    <li className="flex flex-col gap-1 rounded-sm bg-surface-2 px-2 py-1.5">
      <div className="flex items-center gap-2 text-sm">
        <span className="min-w-0 flex-1 truncate">
          {alert.direction === 'crosses_above' ? 'Rises above' : 'Falls below'}{' '}
          <span className="font-bold tabular-nums">{exactPriceRatio(alert.threshold.numerator, alert.threshold.denominator)} USDL</span>
        </span>
        <span className="text-2xs uppercase text-text-faint">{alert.state}</span>
        <IconButton label="Delete alert" icon={<Trash2 />} size="sm" loading={busy} onClick={remove} />
      </div>
      {error ? <FailureNote value={error} title="Deleting alerts" /> : null}
    </li>
  );
}

/** Price alerts for the open pair: IF_ALERT_CREATE / IF_ALERT_DELETE through the session client. */
export function AlertsPanel({ poolAddress }: { poolAddress: string | null }) {
  const { session } = useAccountSession();
  const operations = useSessionOperations();
  const alerts = useSyncExternalStore(subscribeCreated, readCreated, readCreated);
  const wallet = session?.session.wallet ?? null;
  if (!wallet || !operations) {
    return <EmptyState variant="panel" icon={BellRing} title="Price alerts" body="Sign in to get notified when a coin crosses a price."
      action={<Button variant="primary" size="sm" onClick={() => openAuthModal()}>Sign in</Button>} />;
  }
  const pool = poolAddress?.toLowerCase() ?? null;
  const mine = alerts.filter(row => row.wallet.toLowerCase() === wallet.toLowerCase() && (pool === null || row.poolAddress.toLowerCase() === pool));
  return (
    <div className="flex flex-col gap-3">
      {pool ? <AlertForm pool={pool} operations={operations} wallet={wallet} /> : <p className="px-2 text-sm text-text-muted">Open a coin to set an alert for it.</p>}
      <section aria-label="Alerts created this session" className="flex flex-col gap-1 px-2">
        <h3 className="text-xs font-bold text-text-muted">Created this session</h3>
        {mine.length === 0 ? <EmptyState variant="inline" title="No alerts yet" body="Alerts you create show up here." className="text-xs" /> : <ul className="flex flex-col gap-1">{mine.map(alert => <AlertRow key={alert.alertId} alert={alert} operations={operations} />)}</ul>}
        <p className="text-2xs text-text-faint">Only alerts you set in this visit are listed here.</p>
      </section>
    </div>
  );
}
