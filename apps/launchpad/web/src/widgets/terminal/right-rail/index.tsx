'use client';
import { useEffect, useState } from 'react';
import { ChevronDown, Settings } from 'lucide-react';
import { IconButton, SegmentedControl } from '@/ui/primitives';
import StatsAuditPanel from '@/widgets/coin/stats-audit-panel';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useMarketTrades } from '@/page-modules/token/market-session-context';
import { cn } from '@/lib/utils';
import { useTerminalLive } from '../live-context';
import { presetIds, useTradePresets, type PresetId } from '../presets-store';
import { AccordionCard, useAccordion } from './accordion';
import { PositionSummary } from './position-summary';
import { PresetEditor } from './preset-editor';
import { QuickTradeCard } from './quick-trade-card';
import { SimilarSortMenu, SimilarTokensList } from './similar-tokens-list';
import type { SimilarSort } from './similar-tokens';
import { WindowStatsCard } from './window-stats-card';

/** Terminal right column: position, presets + trade card, window stats, token data and similar tokens. */
export default function TerminalRightRail({ poolAddress }: { poolAddress: string }) {
  const pool = poolAddress.toLowerCase();
  const live = useTerminalLive(pool);
  const trades = useMarketTrades(pool);
  const { session } = live;
  useEffect(() => { if (trades.data) session.mergePage(trades.data.data.items, trades.data.revision); }, [trades.data, session]);
  const snapshot = usePoolSnapshot(pool);
  const stats = live.state.stats ?? snapshot.data?.stats ?? null;
  const tokenAddress = snapshot.data?.pool.tokenAddress.toLowerCase() ?? null;
  const symbol = snapshot.data?.token?.symbol?.trim() || snapshot.data?.metadata?.token.symbol?.trim() || null;

  const { active, presets, setActive, updatePreset } = useTradePresets();
  const [editorOpen, setEditorOpen] = useState(false);
  const [tradeOpen, setTradeOpen] = useAccordion('trade');
  const [sort, setSort] = useState<SimilarSort>('liq');

  return (
    <div className="flex flex-col gap-1">
      <PositionSummary poolAddress={pool} tokenAddress={tokenAddress} />

      <section aria-label="Trade" className="flex flex-col gap-1 rounded-md bg-surface-1 p-1">
        <div className="flex h-7 items-center gap-1">
          <IconButton label="Edit trade presets" icon={<Settings />} size="sm" onClick={() => setEditorOpen(true)} />
          <SegmentedControl<`${PresetId}`>
            options={presetIds.map(id => ({ value: `${id}` as `${PresetId}`, label: `P${id}` }))}
            value={`${active}`}
            onChange={value => setActive(Number(value) as PresetId)}
            label="Active trade preset"
            size="sm"
            className="flex-1"
          />
          <IconButton
            label={tradeOpen ? 'Collapse trade card' : 'Expand trade card'}
            icon={<ChevronDown className={cn('transition-transform duration-fast', tradeOpen && 'rotate-180')} />}
            size="sm"
            aria-expanded={tradeOpen}
            onClick={() => setTradeOpen(!tradeOpen)}
          />
        </div>
        {tradeOpen ? <QuickTradeCard poolAddress={pool} preset={presets[active]} onSlippageChange={bps => { updatePreset(active, { slippageBps: bps }); }} /> : null}
      </section>

      <WindowStatsCard poolAddress={pool} fills={live.state.rows} stats={stats} />

      <AccordionCard id="security" title="Coin info and safety">
        <StatsAuditPanel poolAddress={pool} liveStats={stats} />
      </AccordionCard>

      <AccordionCard id="similar" title="Similar coins" trailing={<SimilarSortMenu sort={sort} onChange={setSort} />}>
        <SimilarTokensList poolAddress={pool} symbol={symbol} sort={sort} />
      </AccordionCard>

      <PresetEditor open={editorOpen} onClose={() => setEditorOpen(false)} presets={presets} initial={active} onSave={(id, preset) => { updatePreset(id, preset); }} />
    </div>
  );
}
