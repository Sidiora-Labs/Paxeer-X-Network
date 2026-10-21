'use client';

import { cn } from '@/lib/utils';
import { exactAmount } from './account-disclosure';
import type { IntervalSeries } from './account-transport';
import { MaskedAmount } from './balance-visibility';

function realizedLabel(raw: string | null): { text: string; tone: string } {
  if (raw === null) return { text: '—', tone: 'text-text-faint' };
  const value = BigInt(raw);
  if (value < 0n) return { text: `Loss ${exactAmount(raw)}`, tone: 'text-danger' };
  if (value > 0n) return { text: `Gain ${exactAmount(raw)}`, tone: 'text-success' };
  return { text: 'No realized profit', tone: 'text-text-muted' };
}

export default function NetWorthChart({ series }: { series: IntervalSeries }) {
  return (
    <section
      data-account-view="intervals"
      data-snapshot-id={series.snapshotId}
      data-accounting-revision={series.watermark.projectionRevision}
      aria-label="PnL by period"
      className="flex flex-col gap-2 rounded-md bg-surface-2 p-3"
    >
      <h4 className="text-sm font-bold text-text">PnL by period (UTC)</h4>
      <p className="text-xs text-text-faint">{series.method} · {series.algorithm}</p>
      <p className="text-xs text-text-muted">Realized PnL only counts trades with a known cost. Deposits and withdrawals don't count as profit.</p>
      {series.entries.length === 0 ? (
        <p className="py-3 text-center text-sm text-text-faint">No trades in this period</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-left text-xs">
            <thead className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">
              <tr>
                <th scope="col" className="px-2 py-1.5">Period (UTC)</th>
                <th scope="col" className="px-2 py-1.5">Realized PnL</th>
                <th scope="col" className="px-2 py-1.5">Known gains</th>
                <th scope="col" className="px-2 py-1.5">Untracked sales</th>
                <th scope="col" className="px-2 py-1.5">Network fees</th>
              </tr>
            </thead>
            <tbody>
              {series.entries.map((row, index) => {
                const realized = realizedLabel(row.realizedPnlUsdlRaw);
                return (
                  <tr key={row.start} data-interval-start={row.start} data-interval-end={row.end} className={cn('tabular-nums text-text-muted', index % 2 === 0 && 'bg-surface-3')}>
                    <td className="px-2 py-1.5 font-mono-sl">{row.start} to {row.end}</td>
                    <td className={cn('px-2 py-1.5 font-semibold', realized.tone)}><MaskedAmount>{realized.text}</MaskedAmount></td>
                    <td className="px-2 py-1.5"><MaskedAmount>{exactAmount(row.knownNetGainUsdlRaw)}</MaskedAmount></td>
                    <td className="px-2 py-1.5"><MaskedAmount>{exactAmount(row.unknownProceedsUsdlRaw)}</MaskedAmount></td>
                    <td className="px-2 py-1.5"><MaskedAmount>{exactAmount(row.gasPaxRaw, 18, 'PAX')}</MaskedAmount></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
