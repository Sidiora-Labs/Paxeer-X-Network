'use client';
import { useState } from 'react';
import { Button } from '@/ui/primitives';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { shortAddress } from '@/utils/format';
import { attemptState } from './domain';
import { attemptStateLabel, plainTradeError } from '../trade-copy';
import type { TradeJourney, JourneyState } from './runtime';

const TERMINAL = ['quarantined', 'cancelled', 'replaced', 'changed_action'];
const PAGE_SIZE = 25;
const inputClass = 'h-control-height-sm min-w-0 flex-1 rounded-sm bg-surface-3 px-2 text-xs text-text placeholder:text-text-faint outline-none focus-visible:outline-2 focus-visible:outline-focus';
const summaryClass = 'cursor-pointer select-none font-bold text-text-muted transition-colors duration-fast hover:text-text';

function explorerLink(txHash: string) {
  const href = safeExternalDestination(`https://paxscan.paxeer.app/tx/${txHash}`);
  return href
    ? <a target="_blank" rel="noreferrer" href={href} className="font-mono-sl text-text-muted hover:text-text hover:underline" title={txHash}>{shortAddress(txHash, 6, 4)}</a>
    : <span className="break-all font-mono-sl text-text-muted">{txHash}</span>;
}

export function TransactionStatus({ journey, state }: { journey: TradeJourney | null; state: JourneyState }) {
  const [page, setPage] = useState(0);
  const [replacement, setReplacement] = useState('');
  const [error, setError] = useState('');
  const [recoveryHash, setRecoveryHash] = useState('');
  const [recoveryAction, setRecoveryAction] = useState<'approve' | 'buy' | 'sell'>('buy');

  const attempts = state.attempts.slice(page * PAGE_SIZE, page * PAGE_SIZE + PAGE_SIZE);
  const hasIssues = !!(state.integrityError || state.storageError || state.replacement || error);
  const alwaysOpen = state.attempts.length > 0 || hasIssues;

  return (
    <section aria-label="Recent trades" className="rounded-sm bg-surface-2 text-xs text-text-muted">
      <details open={alwaysOpen || undefined} className="group">
        <summary className={`${summaryClass} flex items-center justify-between px-2.5 py-2`}>
          <span>Recent trades</span>
          {state.attempts.length > 0 ? <span className="tabular-nums text-text-faint">{state.attempts.length}</span> : null}
        </summary>
        <div className="space-y-2 px-2.5 pb-2.5">
          {(state.integrityError || state.storageError) ? <p role="alert" className="rounded-sm bg-danger-soft px-2 py-1.5 text-danger">{plainTradeError(state.integrityError || state.storageError)}</p> : null}
          {state.storageError ? (
            <Button size="sm" variant="secondary" onClick={() => { try { journey?.repairLocalHistory(); } catch (caught) { console.error('[trade] repair', caught); setError(plainTradeError(String(caught))); } }}>
              Clear broken history
            </Button>
          ) : null}

          {attempts.map(update => {
            const terminal = update.receipt || TERMINAL.includes(update.state);
            const status = terminal ? update.state : attemptState(update.attempt, Date.now());
            return (
              <div key={update.attempt.txHash} className="space-y-1.5 rounded-sm bg-surface-3 p-2">
                <div className="flex items-center justify-between gap-3">
                  <p className="font-bold text-text capitalize">{update.attempt.action}: <span className="font-normal text-text-muted">{attemptStateLabel[status] ?? status.replaceAll('_', ' ')}</span></p>
                  {explorerLink(update.attempt.txHash)}
                </div>
                {!terminal ? (
                  <div className="space-y-1.5">
                    <Button size="sm" variant="secondary" onClick={() => journey?.retry(update.attempt.txHash)}>Check again</Button>
                    <details>
                      <summary className={summaryClass}>Sped up or cancelled in your wallet?</summary>
                      <div className="mt-1.5 flex items-center gap-2">
                        <input aria-label="New transaction ID" value={replacement} onChange={event => setReplacement(event.target.value)} className={inputClass} placeholder="Transaction ID (0x...)" />
                        <Button size="sm" variant="secondary" onClick={() => { if (!/^0x[0-9a-fA-F]{64}$/.test(replacement)) { setError('Enter a valid transaction ID'); return; } void journey?.inspectReplacement(update.attempt.txHash, replacement as `0x${string}`).catch(caught => setError(plainTradeError(String(caught)))); }}>
                          Check
                        </Button>
                      </div>
                    </details>
                  </div>
                ) : null}
              </div>
            );
          })}

          {error ? <p role="alert" className="rounded-sm bg-danger-soft px-2 py-1.5 text-danger">{error}</p> : null}

          <details>
            <summary className={summaryClass}>Find a trade by transaction ID</summary>
            <div className="mt-1.5 flex flex-wrap items-center gap-2">
              <input aria-label="Transaction ID" value={recoveryHash} maxLength={66} onChange={event => setRecoveryHash(event.target.value)} className={inputClass} placeholder="Transaction ID (0x...)" />
              <select aria-label="Trade type" value={recoveryAction} onChange={event => setRecoveryAction(event.target.value as 'approve' | 'buy' | 'sell')} className="h-control-height-sm rounded-sm bg-surface-3 px-2 text-xs text-text outline-none focus-visible:outline-2 focus-visible:outline-focus">
                <option value="buy">Buy</option>
                <option value="sell">Sell</option>
                <option value="approve">Approval</option>
              </select>
              <Button size="sm" variant="secondary" disabled={!journey} onClick={() => { if (!/^0x[0-9a-fA-F]{64}$/.test(recoveryHash)) { setError('Enter a valid transaction ID'); return; } void journey?.recoverHash(recoveryHash as `0x${string}`, recoveryAction).catch(caught => setError(plainTradeError(String(caught)))); }}>
                Find trade
              </Button>
            </div>
          </details>

          {state.replacement?.changed ? (
            <div role="alert" className="space-y-1 rounded-sm bg-danger-soft p-2 text-danger">
              <p className="font-bold">This transaction was changed in your wallet. It isn&apos;t the trade you confirmed.</p>
              <p>Sent to: <span className="font-mono-sl">{state.replacement.changed.to}</span></p>
              <p className="tabular-nums">Amount (raw): {state.replacement.changed.value}</p>
              <details>
                <summary className="cursor-pointer select-none font-bold">Details</summary>
                <p className="break-all font-mono-sl">{state.replacement.changed.data}</p>
              </details>
            </div>
          ) : null}
          {state.replacement ? (
            <p className="break-all">
              {attemptStateLabel[state.replacement.state] ?? state.replacement.state.replaceAll('_', ' ')}: <span className="font-mono-sl">{shortAddress(state.replacement.original, 6, 4)}</span> to <span className="font-mono-sl">{shortAddress(state.replacement.replacement, 6, 4)}</span>
            </p>
          ) : null}

          {(page > 0 || (page + 1) * PAGE_SIZE < state.attempts.length) ? (
            <div className="flex items-center justify-between gap-2">
              <Button size="sm" variant="ghost" disabled={page === 0} onClick={() => setPage(page - 1)}>Newer</Button>
              <Button size="sm" variant="ghost" disabled={(page + 1) * PAGE_SIZE >= state.attempts.length} onClick={() => setPage(page + 1)}>Older</Button>
            </div>
          ) : null}
        </div>
      </details>
    </section>
  );
}
