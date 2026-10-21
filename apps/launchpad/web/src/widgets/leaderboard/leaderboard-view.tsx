'use client';
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { UnderlineTabs } from '@/ui/primitives';
import { CupHero } from './cups/cup-hero';
import { CupResultsList } from './cups/cup-results-list';
import { useCupRetry, useCurrentCup } from './cups/use-cup';
import { parsePeriod, PeriodControl, type LeaderboardPeriod } from './period-control';
import { RewardsTab } from './rewards-tab';
import { SquadsTab } from './squads-tab';
import { TradersBoard } from './traders-board';

export type LeaderboardTab = 'traders' | 'squads' | 'rewards' | 'callouts';

const tabIdPrefix = 'leaderboard';

/** Tabs sit flush with the title, labels unpadded with a 20px gap and the underline spanning the label (pump.fun anatomy). */
const tabStripClass = '[&_[role=tablist]]:gap-5 [&_[role=tab]]:px-0 [&_[role=tab]>span[aria-hidden=true]]:inset-x-0';

/**
 * Wide-desktop rail column. Its first row lines up with the page title: the
 * rail's own heading is lifted to the H1 scale and centred on the 56px title row.
 */
const railColumnClass = 'min-w-0 self-start pt-3 xl:sticky xl:top-4 [&>aside>div:first-child_h2]:text-xl';
/** pump.fun keeps Best callouts as its own column only on wide desktops; below xl it is a fourth tab. */
const railQuery = '(min-width: 1280px)';

export function isLeaderboardTab(value: string | null | undefined): value is LeaderboardTab {
  return value === 'traders' || value === 'squads' || value === 'rewards' || value === 'callouts';
}

export function parseTab(value: string | null | undefined, wideViewport: boolean): LeaderboardTab {
  if (!isLeaderboardTab(value)) return 'traders';
  return value === 'callouts' && wideViewport ? 'traders' : value;
}

function subscribeViewport(onChange: () => void) {
  const media = window.matchMedia(railQuery);
  media.addEventListener('change', onChange);
  return () => media.removeEventListener('change', onChange);
}

/** True at >= xl where the callouts rail has its own column; below that it becomes a tab. */
function useWideViewport(): boolean {
  return useSyncExternalStore(subscribeViewport, () => window.matchMedia(railQuery).matches, () => true);
}

function readSearch(): URLSearchParams {
  return new URLSearchParams(window.location.search);
}

function writeSearch(values: Record<string, string>) {
  const url = new URL(window.location.href);
  for (const [key, value] of Object.entries(values)) url.searchParams.set(key, value);
  window.history.replaceState(window.history.state, '', url);
}

export interface LeaderboardViewProps {
  /** Right-rail content (the callouts rail); below xl it renders inside the Best callouts tab. */
  rail?: ReactNode;
  className?: string;
}

/**
 * /leaderboard (layout.md S06): title, underline tabs Traders / Squads /
 * Rewards (+ Best callouts below xl), then the cup hero with the period
 * control inside it, then the board for the chosen period. At xl the callouts
 * rail is a second column whose heading shares the title row.
 * Tab and period are mirrored into ?tab= and ?period=.
 */
export function LeaderboardView({ rail, className }: LeaderboardViewProps) {
  const wide = useWideViewport();
  const [tab, setTab] = useState<LeaderboardTab>('traders');
  const [period, setPeriod] = useState<LeaderboardPeriod>('1d');
  const [explicitPeriod, setExplicitPeriod] = useState(false);
  const cup = useCurrentCup();
  const retryCup = useCupRetry();

  useEffect(() => {
    const restore = () => {
      const search = readSearch();
      setTab(parseTab(search.get('tab'), window.matchMedia(railQuery).matches));
      const chosen = search.get('period');
      setExplicitPeriod(chosen !== null);
      setPeriod(parsePeriod(chosen));
    };
    restore();
    window.addEventListener('popstate', restore);
    window.addEventListener('kindle:restore-navigation', restore);
    return () => { window.removeEventListener('popstate', restore); window.removeEventListener('kindle:restore-navigation', restore); };
  }, []);

  useEffect(() => {
    if (explicitPeriod || cup.state !== 'ready' || cup.data === null) return;
    if (cup.data.status === 'live' || cup.data.status === 'finished') setPeriod('cup');
  }, [cup, explicitPeriod]);

  useEffect(() => {
    if (wide && tab === 'callouts') setTab('traders');
  }, [wide, tab]);

  const changeTab = useCallback((next: LeaderboardTab) => { setTab(next); writeSearch({ tab: next }); }, []);
  const changePeriod = useCallback((next: LeaderboardPeriod) => { setPeriod(next); setExplicitPeriod(true); writeSearch({ period: next }); }, []);

  const tabs = useMemo(() => [
    { id: 'traders' as const, label: 'Traders' },
    { id: 'squads' as const, label: 'Squads' },
    { id: 'rewards' as const, label: 'Rewards' },
    ...(wide || !rail ? [] : [{ id: 'callouts' as const, label: 'Best callouts' }]),
  ], [wide, rail]);

  const cupReady = cup.state === 'ready' && cup.data !== null;
  const cupDisabled = cup.state !== 'loading' && !cupReady;
  // A cup period with no cup record falls back to the rolling day board; nothing is ranked without a record.
  const shownPeriod: LeaderboardPeriod = period === 'cup' && cupDisabled ? '1d' : period;

  const panelProps = (id: LeaderboardTab) => ({ id: `${tabIdPrefix}-panel-${id}`, role: 'tabpanel' as const, 'aria-labelledby': `${tabIdPrefix}-tab-${id}`, tabIndex: 0 });

  return (
    <div className={cn('mx-auto w-full max-w-content-narrow-max-width px-4 pb-16 pt-4 sm:px-6', className)}>
      <div className={cn('grid gap-6', rail && wide && 'xl:grid-cols-[minmax(0,1fr)_var(--spacing-right-rail-width-wide)]')}>
        <div className="min-w-0">
          <h1 className="h-14 text-[22px] font-bold leading-[56px] text-text sm:text-xl sm:leading-[56px]">Leaderboard</h1>
          <UnderlineTabs tabs={tabs} active={tab} onChange={changeTab} label="Leaderboard sections" idPrefix={tabIdPrefix} manual className={tabStripClass} />
          <div className="mt-4">
            {tab === 'traders' ? (
              <div {...panelProps('traders')} className="flex flex-col gap-4">
                <CupHero
                  cup={cup}
                  onRetry={retryCup}
                  control={<PeriodControl value={shownPeriod} onChange={changePeriod} cupDisabled={cupDisabled} />}
                />
                {shownPeriod === 'cup' ? (
                  cup.state === 'ready' && cup.data !== null ? <CupResultsList cup={cup.data} /> : null
                ) : (
                  <TradersBoard period={shownPeriod} />
                )}
              </div>
            ) : null}
            {tab === 'squads' ? <div {...panelProps('squads')}><SquadsTab /></div> : null}
            {tab === 'rewards' ? <div {...panelProps('rewards')}><RewardsTab /></div> : null}
            {tab === 'callouts' && rail ? <div {...panelProps('callouts')}>{rail}</div> : null}
          </div>
        </div>
        {rail && wide ? <div className={railColumnClass}>{rail}</div> : null}
      </div>
    </div>
  );
}
