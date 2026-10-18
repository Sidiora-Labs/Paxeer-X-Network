'use client';
import { useMemo, useState, type ReactNode } from 'react';
import { useAccount } from 'wagmi';
import { Megaphone, Zap } from 'lucide-react';
import { Chip, ErrorState, Skeleton, TabStrip, Toggle, Tooltip, UnavailableState, type TabItem } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { priceNumber } from '@/page-modules/token/market-presentation';
import HoldersTable from '@/widgets/coin/holders-table';
import { HotCalloutsPanel } from '@/widgets/social/hot-callouts-panel';
import { useTerminalLive } from '../live-context';
import { fillFilterOptions, type FillFilter, type FillFilterWallets } from './fills';
import { useInstantTrade } from './instant-trade';
import { OrdersPanel } from './orders-panel';
import { PositionsPanel } from './positions-panel';
import { TradesPanel, type FillsToken } from './trades-panel';
import { userFacingMessage } from '@/core/user-facing-error';

export type ActivityTab = 'trades' | 'positions' | 'orders' | 'holders' | 'top-traders' | 'dev-tokens';
const idPrefix = 'terminal-activity';
const unverifiedToken: FillsToken = { decimals: null, decimalsQuality: 'unavailable' };

/** Holder count from the served stats projection; undefined (hidden) when not served. */
export function holderCountOf(raw: string | null | undefined): number | undefined {
  if (raw === null || raw === undefined || !/^(0|[1-9][0-9]{0,15})$/.test(raw)) return undefined;
  return Number(raw);
}

function FillFilterChips({ value, onChange, wallets }: { value: FillFilter; onChange: (next: FillFilter) => void; wallets: FillFilterWallets }) {
  return (
    <div role="group" aria-label="Fill filter" className="flex items-center gap-1">
      {fillFilterOptions(wallets).map(option => {
        if (option.unavailable) {
          return (
            <Tooltip key={option.value} content={option.unavailable}>
              <Chip pressed={false} aria-disabled="true" onClick={() => undefined} className="h-6 cursor-not-allowed px-2.5 text-xs text-text-disabled hover:bg-surface-2 hover:text-text-disabled">
                {option.label}<span className="sr-only">, unavailable: {option.unavailable}</span>
              </Chip>
            </Tooltip>
          );
        }
        return <Chip key={option.value} pressed={value === option.value} onClick={() => onChange(option.value)} className="h-6 px-2.5 text-xs">{option.label}</Chip>;
      })}
    </div>
  );
}

function SwitchRow({ id, label, icon, checked, onChange }: { id: string; label: string; icon: ReactNode; checked: boolean; onChange: (next: boolean) => void }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-pill bg-surface-2 py-0.5 pl-2 pr-1 text-xs font-bold text-text-muted">
      <span aria-hidden="true" className={cn('inline-flex [&>svg]:size-3.5', checked && 'text-accent')}>{icon}</span>
      <span id={id}>{label}</span>
      <Toggle size="sm" checked={checked} onChange={onChange} labelledBy={id} />
    </span>
  );
}

/** Terminal centre-column activity card: Trades, Positions, Orders, Holders, Top Traders, Dev Tokens. */
export default function TerminalActivityTabs({ poolAddress }: { poolAddress: string }) {
  const pool = poolAddress.toLowerCase();
  const live = useTerminalLive(pool);
  const snapshot = usePoolSnapshot(pool);
  const account = useAccount();
  const [tab, setTab] = useState<ActivityTab>('trades');
  const [filter, setFilter] = useState<FillFilter>('all');
  const [instant, setInstant] = useInstantTrade();
  const [callouts, setCallouts] = useState(false);

  const mine = account.isConnected && account.address ? account.address.toLowerCase() : null;
  const dev = snapshot.data?.creator ? snapshot.data.creator.toLowerCase() : null;
  const wallets = useMemo<FillFilterWallets>(() => ({ mine, dev }), [mine, dev]);
  const activeFilter: FillFilter = fillFilterOptions(wallets).find(option => option.value === filter)?.unavailable ? 'all' : filter;

  const token: FillsToken = snapshot.data?.token ?? unverifiedToken;
  const tokenAddress = snapshot.data?.pool.tokenAddress ?? null;
  const symbol = snapshot.data?.token?.symbol ?? snapshot.data?.metadata?.token.symbol ?? 'Coin';
  const holders = holderCountOf((live.state.stats ?? snapshot.data?.stats)?.holderCount);
  const fillsKnown = live.state.watermark !== null || live.state.rows.length > 0;

  const tabs: TabItem<ActivityTab>[] = [
    { id: 'trades', label: 'Trades', ...(fillsKnown ? { count: live.state.rows.length } : {}) },
    { id: 'positions', label: 'Positions' },
    { id: 'orders', label: 'Orders' },
    { id: 'holders', label: 'Holders', ...(holders !== undefined ? { count: holders } : {}) },
    { id: 'top-traders', label: 'Top Traders' },
    { id: 'dev-tokens', label: 'Creator coins' },
  ];

  function panel() {
    if (tab === 'top-traders') return <UnavailableState title="Top traders" plural operation="GET /v2/pools/{poolAddress}/top-traders" status={null} message="Top traders are coming soon." />;
    if (tab === 'dev-tokens') return <UnavailableState title="Creator coins" plural operation="IF_POOL_LIST GET /v2/pools (creator filter)" status={null} message="Other coins by this creator are coming soon." />;
    if (tab === 'holders') return <div className="p-2"><HoldersTable poolAddress={pool} price={priceNumber(live.state.price)} /></div>;
    if (tab === 'orders') return <OrdersPanel poolAddress={pool} />;
    if (snapshot.isPending) return <Skeleton variant="rows" rows={5} label={tab} />;
    if (snapshot.isError) return <ErrorState variant="inline" title="This coin can't load right now" message={userFacingMessage(snapshot.error)} onRetry={() => { void snapshot.refetch(); }} />;
    if (tab === 'positions') return tokenAddress ? <PositionsPanel tokenAddress={tokenAddress} token={token} symbol={symbol} /> : null;
    return <TradesPanel poolAddress={pool} token={token} live={live} filter={activeFilter} wallets={wallets} />;
  }

  return (
    <section aria-label="Pair activity" className="flex min-h-0 flex-col rounded-md bg-surface-1">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-2 py-1">
        <TabStrip idPrefix={idPrefix} variant="filled" manual label="Pair activity" tabs={tabs} active={tab} onChange={setTab} className="min-w-0 flex-1" />
        <div className="flex flex-wrap items-center gap-2">
          {tab === 'trades' ? <FillFilterChips value={activeFilter} onChange={setFilter} wallets={wallets} /> : null}
          <SwitchRow id={`${idPrefix}-instant`} label="Instant trade" icon={<Zap />} checked={instant} onChange={setInstant} />
          <SwitchRow id={`${idPrefix}-callouts`} label="Show callouts" icon={<Megaphone />} checked={callouts} onChange={setCallouts} />
        </div>
      </div>
      <div className="flex min-h-0 flex-1">
        <div id={`${idPrefix}-panel-${tab}`} role="tabpanel" aria-labelledby={`${idPrefix}-tab-${tab}`} className="min-w-0 flex-1 overflow-auto">
          {panel()}
        </div>
        {callouts
          ? (
            <aside aria-label="Callouts" className="flex w-[340px] max-w-[50%] shrink-0 flex-col overflow-auto bg-surface-2">
              <HotCalloutsPanel poolAddress={pool} initialFeed="coin" />
            </aside>
          )
          : null}
      </div>
    </section>
  );
}
