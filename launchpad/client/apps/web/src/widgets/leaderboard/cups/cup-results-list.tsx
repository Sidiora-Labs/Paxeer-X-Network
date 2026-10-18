'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { ChevronLeft, ChevronRight, Medal } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { Loadable } from '@/core/market/unsupported';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { Avatar, EmptyState, ErrorState, IconButton, RankBadge, SectionHeader, UnavailableState } from '@/ui/primitives';
import { RankRowSkeleton } from '@/ui/shared/skeletons';
import { formatUsdl, relativeAge, shortAddress } from '@/utils/format';
import { FollowButton } from '@/widgets/social/follow-button';
import { profileHref, RankRow } from '../rank-row';
import { cupHighlights, metricTone, prizeForRank, type Cup, type CupStanding } from './cups-contract';
import { useCupResults, useCupRetry, useCupStandings } from './use-cup';

export interface CupResultsListProps {
  cup: Cup;
  className?: string;
}

const toneClass = { success: 'text-success', danger: 'text-danger', neutral: 'text-text' } as const;

interface HighlightsProps { cup: Cup; items: CupStanding[]; identities: ReturnType<typeof useWalletIdentities>['identities'] }

/** Highlight carousel (components.md 39): top five results, avatar fallback variant since no editorial art exists. */
function Highlights({ cup, items, identities }: HighlightsProps) {
  const track = useRef<HTMLUListElement | null>(null);
  const [edges, setEdges] = useState({ start: true, end: true });
  useEffect(() => {
    const node = track.current;
    if (!node) return;
    const measure = () => setEdges({ start: node.scrollLeft <= 1, end: node.scrollLeft + node.clientWidth >= node.scrollWidth - 1 });
    measure();
    node.addEventListener('scroll', measure, { passive: true });
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => { node.removeEventListener('scroll', measure); observer.disconnect(); };
  }, [items.length]);
  const step = (direction: 1 | -1) => { track.current?.scrollBy({ left: direction * 192, behavior: 'smooth' }); };
  if (items.length === 0) return null;
  return (
    <section role="region" aria-roledescription="carousel" aria-labelledby="cup-highlights-title" className="rounded-lg bg-surface-1 p-3 sm:p-4">
      <SectionHeader
        title={<span id="cup-highlights-title" className="text-2xs uppercase tracking-[0.04em] text-text-faint">Cup highlights</span>}
        level={3}
        action={(
          <div className="flex gap-1">
            <IconButton label="Previous highlights" icon={<ChevronLeft />} variant="round" size="sm" disabled={edges.start} onClick={() => step(-1)} />
            <IconButton label="Next highlights" icon={<ChevronRight />} variant="round" size="sm" disabled={edges.end} onClick={() => step(1)} />
          </div>
        )}
      />
      <ul ref={track} className="mt-3 flex snap-x snap-mandatory gap-3 overflow-x-auto pb-1 [scrollbar-width:none]">
        {items.map(row => {
          const wallet = row.wallet.toLowerCase();
          const identity = identities[wallet];
          const name = identity?.displayName?.trim() || shortAddress(wallet, 6, 4);
          return (
            <li key={wallet} className="w-[180px] shrink-0 snap-start">
              <Link href={profileHref(wallet)} className="relative flex aspect-[3/4] flex-col justify-end overflow-hidden rounded-md bg-surface-2 transition-opacity duration-fast hover:opacity-95">
                <RankBadge rank={row.rank} variant="media" className="absolute left-2 top-2" />
                <div aria-hidden="true" className="absolute inset-x-0 top-0 flex h-2/3 items-center justify-center">
                  <Avatar src={identity?.avatarUrl ?? null} name={name} size="2xl" decorative />
                </div>
                <div className="relative flex flex-col gap-1 bg-media-overlay p-3">
                  <span className="truncate text-sm font-bold text-text">{name}</span>
                  <span className={cn('text-sm font-bold tabular-nums', toneClass[metricTone(row.metricSigned)])} data-numeric>{formatUsdl(row.metricSigned, { signed: true })}</span>
                  {prizeForRank(cup, row) !== null ? <span className="text-2xs text-text-muted">Prize {formatUsdl(prizeForRank(cup, row), { fractionDigits: 0 })}</span> : null}
                </div>
              </Link>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

interface StandingsPanelProps { cup: Cup; title: string; state: Loadable<{ items: CupStanding[]; generatedAt: string }>; onRetry: () => void; highlights: boolean }

function StandingsPanel({ cup, title, state, onRetry, highlights }: StandingsPanelProps) {
  const items = state.state === 'ready' ? state.data.items : [];
  const { identities } = useWalletIdentities(items.map(row => row.wallet));
  return (
    <div className="flex flex-col gap-4">
      {highlights && state.state === 'ready' ? <Highlights cup={cup} items={cupHighlights(items)} identities={identities} /> : null}
      <section aria-labelledby="cup-standings-title" className="rounded-lg bg-surface-1 p-3 sm:p-4">
        <SectionHeader title={<span id="cup-standings-title" className="text-2xs uppercase tracking-[0.04em] text-text-faint">{title}</span>} level={3} loading={state.state === 'loading'} />
        <div className="mt-3">
          {state.state === 'loading' ? <RankRowSkeleton count={6} size="lg" /> : null}
          {state.state === 'unsupported' ? <UnavailableState title="Cup standings" operation={state.operation} status={state.status} message="Check back soon" onRetry={onRetry} /> : null}
          {state.state === 'error' ? <ErrorState title="Couldn't load standings" message="Something went wrong, try again" onRetry={state.retry} /> : null}
          {state.state === 'ready' && items.length === 0 ? <EmptyState icon={Medal} title={highlights ? 'No winners' : 'No one has qualified yet'} body={highlights ? 'Nobody qualified for this cup' : 'Standings fill in as people trade during the cup'} /> : null}
          {state.state === 'ready' && items.length > 0 ? (
            <>
              <ol className="flex flex-col gap-0.5" aria-label={title}>
                {items.map(row => {
                  const wallet = row.wallet.toLowerCase();
                  const identity = identities[wallet];
                  return (
                    <RankRow
                      key={wallet}
                      rank={row.rank}
                      wallet={wallet}
                      identity={identity}
                      metricSigned={row.metricSigned}
                      prizeRaw={prizeForRank(cup, row)}
                      size="lg"
                      actions={<FollowButton wallet={wallet} handle={identity?.displayName ?? null} variant="icon" />}
                    />
                  );
                })}
              </ol>
              <p className="mt-3 font-mono text-2xs text-text-faint">Updated {relativeAge(state.data.generatedAt)} ago</p>
            </>
          ) : null}
        </div>
      </section>
    </div>
  );
}

function FinalResults({ cup }: { cup: Cup }) {
  const results = useCupResults(cup.cupId);
  const retry = useCupRetry(cup.cupId);
  return <StandingsPanel cup={cup} title="Final results" state={results} onRetry={retry} highlights />;
}

function LiveStandings({ cup }: { cup: Cup }) {
  const standings = useCupStandings(cup.cupId);
  const retry = useCupRetry(cup.cupId);
  return <StandingsPanel cup={cup} title="Live standings" state={standings} onRetry={retry} highlights={false} />;
}

/**
 * Cup body below the hero: finished cups show the frozen IF_CUP_RESULTS list
 * with the highlight carousel; live cups show IF_CUP_STANDINGS; scheduled cups
 * have nothing to rank yet.
 */
export function CupResultsList({ cup, className }: CupResultsListProps) {
  if (cup.status === 'scheduled') {
    return (
      <section className={cn('rounded-lg bg-surface-1', className)}>
        <EmptyState icon={Medal} title="Standings open when the cup starts" body="Trades count from the moment the cup starts" />
      </section>
    );
  }
  return <div className={className}>{cup.status === 'finished' ? <FinalResults cup={cup} /> : <LiveStandings cup={cup} />}</div>;
}
