'use client';
import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { CircleCheck, CircleQuestionMark, CircleX } from 'lucide-react';
import { KeyValueRow, PairedBar, SegmentedControl, Skeleton, TabStrip, UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import type { PoolStats } from '@/core/market/pool-resolver';
import { useCandles, type Candle, type CandleInterval } from '@/core/market/use-candles';
import { countNumber } from '@/core/market/market-units';
import { formatShare, statsChange, statsShare, type StatValue } from '@/core/market/pool-stats-display';
import { StatChange, StatDash } from '@/ui/shared/stat-value';
import { loadable, toUnsupported, unsupported } from '@/core/market/unsupported';
import { queryKeys } from '@/core/query-keys';
import { statsApiOrigin, statsApiUrl } from '@/core/sdk-config';
import { priceNumber } from '@/page-modules/token/market-presentation';
import { formatCount, formatUsdl, relativeAge } from '@/utils/format';
import { userFacingMessage } from '@/core/user-facing-error';

type Window = '5m' | '1h' | '24h';
type Tab = 'stats' | 'audit';

const windows: Record<Window, { interval: CandleInterval; ms: number; limit: number }> = {
  '5m': { interval: '1m', ms: 5 * 60_000, limit: 10 },
  '1h': { interval: '5m', ms: 60 * 60_000, limit: 20 },
  '24h': { interval: '1h', ms: 24 * 60 * 60_000, limit: 40 },
};
const windowOptions = (Object.keys(windows) as Window[]).map(value => ({ value, label: value }));
const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 2 });

/** Minute-aligned clock so candle query bounds stay stable between renders. */
function useMinuteClock(): number {
  const [minute, setMinute] = useState(() => Math.ceil(Date.now() / 60_000) * 60_000);
  useEffect(() => {
    const tick = () => setMinute(Math.ceil(Date.now() / 60_000) * 60_000);
    const timer = window.setInterval(tick, 60_000);
    return () => window.clearInterval(timer);
  }, []);
  return minute;
}

interface Aggregate { buyVolume: bigint; sellVolume: bigint; volume: bigint; trades: bigint; priceChangePct: number | null }

function aggregate(candles: readonly Candle[]): Aggregate {
  let buyVolume = 0n, sellVolume = 0n, volume = 0n, trades = 0n;
  for (const candle of candles) {
    buyVolume += BigInt(candle.buyVolumeUsdlRaw);
    sellVolume += BigInt(candle.sellVolumeUsdlRaw);
    volume += BigInt(candle.volumeUsdlRaw);
    trades += BigInt(candle.trades);
  }
  const ordered = [...candles].sort((a, b) => a.start.localeCompare(b.start));
  const first = ordered[0], last = ordered[ordered.length - 1];
  const open = first ? priceNumber(first.open) : null, close = last ? priceNumber(last.close) : null;
  const priceChangePct = open && close !== null && open > 0 ? ((close - open) / open) * 100 : null;
  return { buyVolume, sellVolume, volume, trades, priceChangePct };
}

function usdlUnits(raw: bigint): number { return Number(raw) / 1e6; }
function formatUnits(n: number): string { return compact.format(n); }

/* ------------------------------------------------------------- audit */

interface RiskBreakdown { riskRating: number; riskLevel: 'low' | 'medium' | 'high'; riskFactors: string[]; details: { holderCount: number | null; top10ConcentrationPct: number | null; creatorHoldingsPct: number | null; hasCreatorSold: boolean; isNew: boolean; ageSeconds: number | null } }

class RiskRequestError extends Error {
  constructor(readonly status: number) { super(`GET /stats/{p}/risk answered ${status}`); }
}

function numberOrNull(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '' && Number.isFinite(Number(value))) return Number(value);
  return null;
}

function parseRisk(value: unknown): RiskBreakdown {
  if (!value || typeof value !== 'object') throw new TypeError('Risk breakdown is not an object');
  const row = value as Record<string, unknown>;
  const details = (row.details && typeof row.details === 'object' ? row.details : {}) as Record<string, unknown>;
  const rating = numberOrNull(row.riskRating);
  if (rating === null || !['low', 'medium', 'high'].includes(String(row.riskLevel))) throw new TypeError('Risk breakdown shape disagreement');
  const factors = Array.isArray(row.riskFactors) ? row.riskFactors.filter((item): item is string => typeof item === 'string') : [];
  return {
    riskRating: rating,
    riskLevel: row.riskLevel as RiskBreakdown['riskLevel'],
    riskFactors: factors,
    details: {
      holderCount: numberOrNull(details.holderCount),
      top10ConcentrationPct: numberOrNull(details.top10ConcentrationPct),
      creatorHoldingsPct: numberOrNull(details.creatorHoldingsPct),
      hasCreatorSold: details.hasCreatorSold === true,
      isNew: details.isNew === true,
      ageSeconds: numberOrNull(details.ageSeconds),
    },
  };
}

// GET /stats/{p}/risk is served by core/stats-workers only (pool_analytics.go);
// core/api has no such route, so without a stats host the request is not made.
async function fetchRisk(pool: string, signal: AbortSignal): Promise<RiskBreakdown> {
  const url = statsApiUrl(`/stats/${pool}/risk`);
  if (!url) throw new Error('No stats host is configured');
  const response = await fetch(url, { signal, headers: { accept: 'application/json' } });
  if (!response.ok) throw new RiskRequestError(response.status);
  return parseRisk(await response.json());
}

/** Holder facts the v2 statistics serve, for when the risk breakdown route is not; a share with no value carries its reason. */
interface ServedAudit { holderCount: number | null; top10Share: StatValue<number>; creatorShare: StatValue<number>; ageSeconds: number | null }

function auditFromStats(stats: PoolStats | null, createdAt: string | null, now: number): ServedAudit {
  const created = createdAt ? Date.parse(createdAt) : NaN;
  return {
    holderCount: countNumber(stats?.holderCount),
    top10Share: statsShare(stats, 'top10'),
    creatorShare: statsShare(stats, 'creator'),
    ageSeconds: Number.isFinite(created) ? Math.max(0, Math.floor((now - created) / 1000)) : null,
  };
}

type Verdict = 'pass' | 'fail' | 'unknown';

function CheckIcon({ verdict }: { verdict: Verdict }) {
  if (verdict === 'pass') return <CircleCheck aria-hidden="true" className="size-icon-sm text-accent" />;
  if (verdict === 'fail') return <CircleX aria-hidden="true" className="size-icon-sm text-danger" />;
  return <CircleQuestionMark aria-hidden="true" className="size-icon-sm text-text-faint" />;
}

function CheckRow({ label, verdict, value }: { label: string; verdict: Verdict; value: string | null }) {
  const spoken = verdict === 'pass' ? 'pass' : verdict === 'fail' ? 'flagged' : 'unknown';
  return (
    <KeyValueRow
      label={label}
      value={value === null ? null : <span className="inline-flex items-center gap-1.5"><CheckIcon verdict={verdict} /><span className="sr-only">{spoken}: </span>{value}</span>}
    />
  );
}

function percent(value: number | null): string | null {
  return value === null ? null : `${value.toFixed(2)}%`;
}

function AuditRows({ risk, now }: { risk: RiskBreakdown; now: number }) {
  const { details } = risk;
  const levelVerdict: Verdict = risk.riskLevel === 'low' ? 'pass' : 'fail';
  const top10: Verdict = details.top10ConcentrationPct === null ? 'unknown' : details.top10ConcentrationPct > 50 ? 'fail' : 'pass';
  const dev: Verdict = details.creatorHoldingsPct === null ? 'unknown' : details.creatorHoldingsPct > 20 ? 'fail' : 'pass';
  const age = details.ageSeconds === null ? null : relativeAge(now - details.ageSeconds * 1000, now);
  return (
    <dl aria-label="Audit checks" className="flex flex-col gap-2 rounded-md bg-surface-2 px-3 py-1">
      <CheckRow label="Risk rating" verdict={levelVerdict} value={`${risk.riskRating} · ${risk.riskLevel}`} />
      <CheckRow label="Top 10 holders" verdict={top10} value={percent(details.top10ConcentrationPct)} />
      <CheckRow label="Creator holdings" verdict={dev} value={percent(details.creatorHoldingsPct)} />
      <CheckRow label="Creator sold" verdict={details.hasCreatorSold ? 'fail' : 'pass'} value={details.hasCreatorSold ? 'Yes' : 'No'} />
      <CheckRow label="New coin" verdict={details.isNew ? 'fail' : 'pass'} value={details.isNew ? 'Yes' : 'No'} />
      <CheckRow label="Holders" verdict={details.holderCount === null ? 'unknown' : details.holderCount >= 10 ? 'pass' : 'fail'} value={details.holderCount === null ? null : formatCount(details.holderCount)} />
      <CheckRow label="Age" verdict={age === null ? 'unknown' : details.isNew ? 'fail' : 'pass'} value={age} />
      {risk.riskFactors.length ? <KeyValueRow label="Factors" value={<span className="font-mono text-xs">{risk.riskFactors.join(', ')}</span>} /> : null}
    </dl>
  );
}

function ShareCheckRow({ label, share, limit }: { label: string; share: StatValue<number>; limit: number }) {
  if (share.value === null) return <KeyValueRow label={label} value={<span className="inline-flex items-center gap-1.5"><CheckIcon verdict="unknown" /><StatDash reason={share.reason ?? 'Not available yet'} subject={label} /></span>} />;
  return <CheckRow label={label} verdict={share.value > limit ? 'fail' : 'pass'} value={formatShare(share.value)} />;
}

function ServedAuditRows({ audit, now }: { audit: ServedAudit; now: number }) {
  const age = audit.ageSeconds === null ? null : relativeAge(now - audit.ageSeconds * 1000, now);
  return (
    <dl aria-label="Audit checks" className="flex flex-col gap-2 rounded-md bg-surface-2 px-3 py-1">
      <ShareCheckRow label="Top 10 holders" share={audit.top10Share} limit={50} />
      <ShareCheckRow label="Creator holdings" share={audit.creatorShare} limit={20} />
      <CheckRow label="Holders" verdict={audit.holderCount === null ? 'unknown' : audit.holderCount >= 10 ? 'pass' : 'fail'} value={audit.holderCount === null ? null : formatCount(audit.holderCount)} />
      <KeyValueRow label="Age" value={age} />
    </dl>
  );
}

function AuditTab({ poolAddress, now }: { poolAddress: string; now: number }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const pool = snapshot.data?.pool.poolAddress ?? null;
  const statsHost = statsApiOrigin() !== null;
  const risk = useQuery({
    queryKey: queryKeys.tokenRisk(pool ?? ''),
    queryFn: ({ signal }) => fetchRisk(pool as string, signal),
    enabled: pool !== null && statsHost,
    staleTime: 30_000,
    retry: false,
  });
  if (snapshot.isPending || (pool && statsHost && risk.isPending)) return <Skeleton variant="rows" rows={5} label="audit checks" />;
  if (snapshot.isError || !snapshot.data) return <UnavailableState variant="inline" title="Audit" operation="IF_POOL_STATS" status={null} />;
  if (risk.data) return <AuditRows risk={risk.data} now={now} />;
  const marker = !statsHost ? unsupported('GET /stats/{p}/risk', null, 'No stats host is configured on this deployment') : risk.error ? toUnsupported('GET /stats/{p}/risk', risk.error) : null;
  const stats = snapshot.data.stats;
  // Without the risk breakdown, the v2 pool statistics still serve holders and the top-10 and creator shares.
  return (
    <div className="flex flex-col gap-2">
      {stats ? <ServedAuditRows audit={auditFromStats(stats, snapshot.data.createdAt, now)} now={now} /> : null}
      {marker ? <UnavailableState variant="inline" title={stats ? 'Risk checks' : 'Audit'} operation={marker.operation} status={marker.status} /> : null}
      {risk.error && !marker ? <p role="alert" className="text-sm text-danger">Risk checks can&apos;t load right now. {userFacingMessage(risk.error)}</p> : null}
      {!stats && !marker && !risk.error ? <UnavailableState variant="inline" title="Audit" operation={snapshot.data.unavailable.stats?.operation ?? 'IF_STATS'} status={snapshot.data.unavailable.stats?.status ?? null} /> : null}
    </div>
  );
}

/* ------------------------------------------------------------- stats */

/**
 * Frame of a split bar whose values are not served: the label row with dashes over an empty track, so
 * the card keeps pump.fun's three-bar anatomy instead of turning into prose. The reason stays
 * available to assistive tech through the shared unavailable state.
 */
function SplitBarFrame({ leftLabel, rightLabel, title, operation, loading = false }: { leftLabel: string; rightLabel: string; title: string; operation: string; loading?: boolean }) {
  return (
    <div className="flex flex-col gap-1" data-split-frame={leftLabel}>
      <div aria-hidden="true" className="flex items-center justify-between text-xs">
        <span className="text-text-faint">{loading ? <span className="sl-skeleton inline-block h-3 w-8 align-middle" /> : <span className="font-bold text-text-faint">—</span>} {leftLabel}</span>
        <span className="text-text-faint">{loading ? <span className="sl-skeleton inline-block h-3 w-8 align-middle" /> : <span className="font-bold text-text-faint">—</span>} {rightLabel}</span>
      </div>
      <div aria-hidden="true" className="h-1 w-full rounded-pill bg-track" />
      {loading ? <span className="sr-only">Loading {title.toLowerCase()}</span> : <UnavailableState variant="inline" title={title} plural="auto" operation={operation} status={null} className="sr-only" />}
    </div>
  );
}

function StatsTab({ poolAddress, window: selected, now, liveStats }: { poolAddress: string; window: Window; now: number; liveStats: PoolStats | null }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const spec = windows[selected];
  const pool = snapshot.data?.pool.poolAddress ?? null;
  const candles = useCandles(pool, spec.interval, { from: now - spec.ms, to: now, limit: spec.limit });
  // Candles are keyed by the resolved pool: with no pool once resolution settled, the query never runs.
  const series = !pool && !snapshot.isPending ? null : loadable(candles, 'IF_CANDLES');
  const agg = useMemo(() => (series?.state === 'ready' ? aggregate(series.data.data.items) : null), [series]);
  const stats = liveStats ?? snapshot.data?.stats ?? null;
  const seriesLoading = series?.state === 'loading';
  const loading = snapshot.isPending || seriesLoading;

  // The v2 window change (value only on a complete baseline, else its reason); the candle aggregate only without a stats row.
  const change: StatValue<number> | null = stats ? statsChange(stats, selected) : agg?.priceChangePct !== null && agg?.priceChangePct !== undefined ? { value: agg.priceChangePct, reason: null } : null;
  const traders = selected === '24h' && stats ? countNumber(stats.uniqueTraders24h) : null;
  const txns = selected === '24h' && stats ? countNumber(stats.txCount24h) : null;
  const volumeRaw = selected === '24h' && stats ? BigInt(stats.volume24hRaw) : agg ? agg.volume : null;
  const buys = selected === '24h' && stats ? countNumber(stats.buys24h) : null;
  const sells = selected === '24h' && stats ? countNumber(stats.sells24h) : null;

  return (
    <div className="flex flex-col gap-3">
      <dl className="flex items-center justify-between gap-2 text-xs">
        <div className="flex min-w-0 items-center gap-1.5">
          <dt className="text-text-faint">Price change</dt>
          <dd aria-busy={(loading && change === null) || undefined}>
            {loading && change === null ? <span aria-hidden="true" className="sl-skeleton inline-block h-4 w-14 align-middle" /> : change === null ? <span aria-label="No data" className="font-bold text-text-faint">—</span> : <StatChange change={change} variant="text" size="sm" align="start" decimals={2} />}
          </dd>
        </div>
        <div className="flex min-w-0 items-center gap-1.5">
          <dt className="text-text-faint">Volume</dt>
          <dd data-numeric className={`text-sm font-bold tabular-nums ${volumeRaw === null ? 'text-text-faint' : 'text-text'}`} aria-busy={(loading && volumeRaw === null) || undefined}>
            {loading && volumeRaw === null ? <span aria-hidden="true" className="sl-skeleton inline-block h-4 w-14 align-middle" /> : volumeRaw === null ? <span aria-label="No data">—</span> : <>{formatUsdl(volumeRaw, { compact: true, unit: false })} <span className="text-xs font-normal text-text-faint">USDL</span></>}
          </dd>
        </div>
      </dl>
      <div className="flex flex-col gap-3">
        {buys !== null && sells !== null ? <PairedBar left={buys} right={sells} leftLabel="Buys" rightLabel="Sells" /> : (
          <div className="flex flex-col gap-1">
            {agg ? <dl><KeyValueRow className="min-h-8 py-0.5" label="Trades" value={formatCount(agg.trades)} /></dl> : null}
            <SplitBarFrame leftLabel="buys" rightLabel="sells" title={`Buy and sell counts for ${selected}`} operation="IF_CANDLES.trades (not split by side)" loading={snapshot.isPending} />
          </div>
        )}
        {agg ? <PairedBar left={usdlUnits(agg.buyVolume)} right={usdlUnits(agg.sellVolume)} leftLabel="Buy vol" rightLabel="Sell vol" format={formatUnits} /> : null}
        {!agg && series?.state !== 'error' ? <SplitBarFrame leftLabel="buy vol" rightLabel="sell vol" title="Buy and sell volume" operation={series?.state === 'unsupported' ? series.operation : 'IF_CANDLES'} loading={loading} /> : null}
        {series?.state === 'error' ? <p role="alert" className="text-xs text-danger">Buy and sell volume can&apos;t load right now. {userFacingMessage(series.error)}</p> : null}
        {traders !== null || txns !== null ? (
          <dl className="flex items-center justify-between gap-2 text-xs">
            <div className="flex items-baseline gap-1.5"><dt className="text-text-faint">Traders</dt><dd data-numeric className="text-sm font-bold tabular-nums text-text">{traders === null ? '—' : formatCount(traders)}</dd></div>
            <div className="flex items-baseline gap-1.5"><dt className="text-text-faint">Txns</dt><dd data-numeric className="text-sm font-bold tabular-nums text-text">{txns === null ? '—' : formatCount(txns)}</dd></div>
          </dl>
        ) : null}
        <SplitBarFrame leftLabel="buyers" rightLabel="sellers" title="Buyers versus sellers" operation="PoolStats.uniqueTraders24h (not split by side)" loading={snapshot.isPending} />
      </div>
    </div>
  );
}

/** Stats (5m / 1h / 24h) and Audit tabs for the right rail. */
export default function StatsAuditPanel({ poolAddress, liveStats }: { poolAddress: string; liveStats: PoolStats | null }) {
  const [tab, setTab] = useState<Tab>('stats');
  const [range, setRange] = useState<Window>('24h');
  const now = useMinuteClock();
  return (
    <section aria-label="Stats and audit" className="flex flex-col gap-3 rounded-md bg-surface-1 p-3">
      <TabStrip<Tab>
        tabs={[{ id: 'stats', label: 'Stats' }, { id: 'audit', label: 'Audit' }]}
        active={tab}
        onChange={setTab}
        label="Stats and audit"
        variant="filled"
        idPrefix="coin-stats"
        trailing={tab === 'stats' ? <SegmentedControl<Window> options={windowOptions} value={range} onChange={setRange} label="Stats window" size="sm" /> : null}
      />
      <div role="tabpanel" id={`coin-stats-panel-${tab}`} aria-labelledby={`coin-stats-tab-${tab}`}>
        {tab === 'stats' ? <StatsTab poolAddress={poolAddress} window={range} now={now} liveStats={liveStats} /> : <AuditTab poolAddress={poolAddress} now={now} />}
      </div>
    </section>
  );
}
