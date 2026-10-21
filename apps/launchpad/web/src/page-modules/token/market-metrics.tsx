'use client';
import { exactUnits, holderRows, type HoldersResponse, type PoolStats, type Token } from './market-presentation';

const noteClass = 'text-xs text-text-faint';

export function MarketStats({ poolAddress, stats }: { poolAddress: string; stats: PoolStats | null; }) {
  if(!stats || stats.poolAddress.toLowerCase() !== poolAddress.toLowerCase() || stats.watermark.integrity === 'quarantined') return <section aria-label="Market statistics" role="status" className="text-sm text-text-faint">Market statistics unavailable: verified snapshot required</section>;
  const change = stats.changeBaselineQuality === 'complete' && stats.priceChangeBps24h !== null ? `${stats.priceChangeBps24h.startsWith('-') ? '-' : '+'}${exactUnits(stats.priceChangeBps24h.replace(/^-/, ''), 2, 2)}%` : '—';
  return <section data-market-stats-pool={poolAddress.toLowerCase()} data-market-stats-revision={stats.watermark.projectionRevision} data-market-window-start={stats.windowStart} data-market-window-end={stats.windowEnd} aria-label="Market statistics" className="flex flex-col gap-2 rounded-md bg-surface-2 px-3 py-2.5 text-text">
    <dl className="grid grid-cols-2 gap-2 sm:grid-cols-3 [&_dd]:text-sm [&_dd]:font-bold [&_dd]:tabular-nums [&_dt]:text-2xs [&_dt]:font-bold [&_dt]:uppercase [&_dt]:tracking-[.04em] [&_dt]:text-text-faint">
      <div><dt>Volume · USDL · 24h</dt><dd>{exactUnits(stats.volume24hRaw, 6)}</dd></div>
      <div><dt>Real pool backing · USDL · snapshot</dt><dd>{exactUnits(stats.realUsdlBalanceRaw, 6)}</dd></div>
      <div><dt>Indicative TVL · USDL · snapshot</dt><dd>{exactUnits(stats.indicativeTvlUsdlRaw, 6)}</dd></div>
      <div><dt>Price change · 24h</dt><dd>{change}</dd></div>
      <div><dt>Buys · fills · 24h</dt><dd>{exactUnits(stats.buys24h, 0)}</dd></div>
      <div><dt>Sells · fills · 24h</dt><dd>{exactUnits(stats.sells24h, 0)}</dd></div>
    </dl>
    <p className={noteClass}>Window: {stats.windowStart} to {stats.windowEnd}</p>
    <p className={noteClass}>Source: pool statistics · height {stats.sourceUpperPosition.height} · revision {stats.watermark.projectionRevision} · observed {stats.watermark.observedAt} · integrity {stats.watermark.integrity}</p>
  </section>;
}

/** Transfer-coverage holder table for the HoldersResponse contract; the coin page uses widgets/coin/holders-table instead. */
export function MarketHolders({ response, token }: { response: HoldersResponse | null; token: Token | null; }) {
  if(!response || !token) return <section aria-label="Token holders" role="status" className="text-sm text-text-faint">Holder balances unavailable: verified snapshot and token identity required</section>;
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') return <section aria-label="Token holders" role="alert" className="text-sm text-danger">Holder balances unavailable: integrity quarantine</section>;
  const rows = holderRows(response);
  const matches = rows.every(row => row.tokenAddress.toLowerCase() === token.address.toLowerCase());
  if(!matches) return <section aria-label="Token holders" role="alert" className="text-sm text-danger">Holder token identity disagreement</section>;
  const complete = response.data.coverage === 'complete' && response.quality === 'complete';
  return <section data-market-list="holders" data-market-holder-coverage={response.data.coverage} data-market-holder-revision={response.revision ?? ''} aria-label="Token holders" className="flex flex-col gap-2 rounded-md bg-surface-2 p-3 text-text">
    <p className={noteClass}>Coverage: {response.data.coverage} · snapshot quality: {response.quality}</p>
    <p className={noteClass}>Share denominator: unavailable in this response; concentration percentages unavailable</p>
    <p className={noteClass}>Excluded addresses policy: {response.data.exclusionPolicyVersion}</p>
    <p className={noteClass}>Source: token transfer projection · height {response.watermark?.height ?? '—'} · revision {response.revision ?? '—'} · generated {response.generatedAt}</p>
    {!complete && <p role="status" className="text-xs text-warning">Incomplete coverage: these balances do not establish total ownership.</p>}
    {rows.length ? <div className="max-h-[350px] overflow-auto"><table className="w-full text-sm [&_td]:px-2 [&_td]:py-1.5 [&_th]:px-2 [&_th]:text-left [&_th]:text-2xs [&_th]:font-bold [&_th]:uppercase [&_th]:tracking-[.04em] [&_th]:text-text-faint"><thead><tr><th scope="col">Holder address</th><th scope="col">Balance · {token.symbol || 'tokens'}</th><th scope="col">Share</th></tr></thead><tbody>{rows.map(row => <tr key={row.wallet} className="bg-surface-2 hover:bg-surface-hover"><td className="break-all font-mono text-xs">{row.wallet}</td><td className="tabular-nums">{exactUnits(row.balanceRaw, token.decimalsQuality === 'verified' ? token.decimals : null)}</td><td className="text-text-faint">—</td></tr>)}</tbody></table></div> : <p className="text-sm text-text-faint">{complete ? 'No holders in this snapshot' : 'No holder rows available in this partial snapshot'}</p>}
    {response.nextCursor && <p className={noteClass}>More holders are available through the snapshot cursor; this page is bounded at 100 rows.</p>}
  </section>;
}
